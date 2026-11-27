package provider

import (
	"context"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/types"

	phronv1 "github.com/phron-net/api/gen/zonhoren/phron/v1"
	"github.com/phron-net/api/gen/zonhoren/phron/v1/phronv1connect"
)

type fakeServerService struct {
	phronv1connect.UnimplementedServerServiceHandler

	mu          sync.Mutex
	servers     map[string]*phronv1.Server
	byRequest   map[string]string
	gets        map[string]int
	creates     int
	failCreates int // answer this many CreateServer calls with Unavailable first
	failBuild   bool
	lastCreate  *phronv1.CreateServerRequest
}

func newFakeServerService() *fakeServerService {
	return &fakeServerService{servers: map[string]*phronv1.Server{}, byRequest: map[string]string{}, gets: map[string]int{}}
}

func (f *fakeServerService) CreateServer(_ context.Context, req *connect.Request[phronv1.CreateServerRequest]) (*connect.Response[phronv1.CreateServerResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failCreates > 0 {
		f.failCreates--
		return nil, connect.NewError(connect.CodeUnavailable, errFake)
	}
	m := req.Msg
	f.lastCreate = m
	if id, ok := f.byRequest[m.GetRequestId()]; ok {
		return connect.NewResponse(&phronv1.CreateServerResponse{Server: f.servers[id]}), nil
	}
	f.creates++
	s := &phronv1.Server{
		Id:              "srv-1",
		Name:            m.GetName(),
		State:           phronv1.ServerState_SERVER_STATE_REQUESTED,
		OperatingSystem: &phronv1.OperatingSystem{Id: m.GetOperatingSystemId()},
		SshKeyIds:       m.GetSshKeyIds(),
		Size:            m.GetSize(),
		Location:        m.GetLocation(),
		WorkspaceId:     "ws-default",
		Ipv6Prefix:      "2001:db8:1::/64",
		SshAddress:      "2001:db8:1::1",
	}
	f.servers[s.Id] = s
	f.byRequest[m.GetRequestId()] = s.Id
	return connect.NewResponse(&phronv1.CreateServerResponse{Server: s}), nil
}

func (f *fakeServerService) GetServer(_ context.Context, req *connect.Request[phronv1.GetServerRequest]) (*connect.Response[phronv1.GetServerResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := req.Msg.GetServerId()
	s, ok := f.servers[id]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errFake)
	}
	f.gets[id]++
	switch s.State {
	case phronv1.ServerState_SERVER_STATE_REQUESTED:
		if f.failBuild {
			s.State = phronv1.ServerState_SERVER_STATE_FAILED
		} else if f.gets[id] >= 2 {
			s.State = phronv1.ServerState_SERVER_STATE_RUNNING
			s.Power = phronv1.ServerPower_SERVER_POWER_RUNNING
		}
	case phronv1.ServerState_SERVER_STATE_DELETING:
		if f.gets[id] >= 2 {
			delete(f.servers, id)
			return nil, connect.NewError(connect.CodeNotFound, errFake)
		}
	}
	return connect.NewResponse(&phronv1.GetServerResponse{Server: s}), nil
}

func (f *fakeServerService) ResizeServer(_ context.Context, req *connect.Request[phronv1.ResizeServerRequest]) (*connect.Response[phronv1.ResizeServerResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.servers[req.Msg.GetServerId()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errFake)
	}
	s.Size = req.Msg.GetSize()
	return connect.NewResponse(&phronv1.ResizeServerResponse{Server: s}), nil
}

func (f *fakeServerService) DeleteServer(_ context.Context, req *connect.Request[phronv1.DeleteServerRequest]) (*connect.Response[phronv1.DeleteServerResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.servers[req.Msg.GetServerId()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errFake)
	}
	s.State = phronv1.ServerState_SERVER_STATE_DELETING
	f.gets[s.Id] = 0
	return connect.NewResponse(&phronv1.DeleteServerResponse{Server: s}), nil
}

func testServerModel() *serverModel {
	return &serverModel{
		ID:                  types.StringUnknown(),
		Name:                types.StringValue("web"),
		OperatingSystemID:   types.StringValue("debian-13"),
		SSHKeyIDs:           stringsToSet([]string{"key-1"}),
		Location:            types.StringValue("fsn"),
		WorkspaceID:         types.StringUnknown(),
		Tier:                types.StringValue("shared"),
		VCPUs:               types.Int64Value(2),
		MemoryGiB:           types.Int64Value(4),
		DiskGiB:             types.Int64Value(40),
		UserData:            types.StringValue("#cloud-config\npackages: [htop]\n"),
		RestoreFromBackupID: types.StringNull(),
		State:               types.StringUnknown(),
		Power:               types.StringUnknown(),
		IPv6Prefix:          types.StringUnknown(),
		SSHAddress:          types.StringUnknown(),
	}
}

func TestServerLifecycle(t *testing.T) {
	ctx := context.Background()
	fake := newFakeServerService()
	r := &serverResource{clients: newTestClients(t, fake, nil)}

	m := testServerModel()
	if err := r.doCreate(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.ID.ValueString() != "srv-1" || m.State.ValueString() != "running" {
		t.Fatalf("after create: id=%q state=%q", m.ID.ValueString(), m.State.ValueString())
	}
	if m.WorkspaceID.ValueString() != "ws-default" || m.SSHAddress.ValueString() != "2001:db8:1::1" {
		t.Fatalf("computed attributes not filled: %+v", m)
	}
	if fake.lastCreate.GetRequestId() == "" || fake.lastCreate.GetUserData() == "" || fake.lastCreate.GetLocation() != "fsn" {
		t.Fatalf("create request lost fields: %+v", fake.lastCreate)
	}
	if got := fake.lastCreate.GetSize(); got.GetTier() != phronv1.ServerTier_SERVER_TIER_SHARED || got.GetVcpus() != 2 || got.GetDiskGib() != 40 {
		t.Fatalf("size not mapped: %+v", got)
	}

	// Read maps the API state back and keeps the configured user data.
	read := *m
	gone, err := r.doRead(ctx, &read)
	if err != nil || gone {
		t.Fatalf("read: gone=%v err=%v", gone, err)
	}
	if read.Name.ValueString() != "web" || read.VCPUs.ValueInt64() != 2 || read.Tier.ValueString() != "shared" || read.UserData.IsNull() {
		t.Fatalf("read mapped wrongly: %+v", read)
	}

	// Resize in place.
	plan := *m
	plan.VCPUs, plan.DiskGiB = types.Int64Value(4), types.Int64Value(80)
	if err := r.doUpdate(ctx, &plan, m); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := fake.servers["srv-1"].GetSize(); got.GetVcpus() != 4 || got.GetDiskGib() != 80 {
		t.Fatalf("resize not sent: %+v", got)
	}

	// Delete waits until the Server is gone; a second delete is not an error,
	// and a read then reports it gone (NotFound removes it from state).
	if err := r.doDelete(ctx, "srv-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := r.doDelete(ctx, "srv-1"); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	gone, err = r.doRead(ctx, &read)
	if err != nil || !gone {
		t.Fatalf("read after delete: gone=%v err=%v", gone, err)
	}
}

func TestServerCreateRetryIsIdempotent(t *testing.T) {
	fake := newFakeServerService()
	fake.failCreates = 2
	r := &serverResource{clients: newTestClients(t, fake, nil)}
	m := testServerModel()
	if err := r.doCreate(context.Background(), m); err != nil {
		t.Fatalf("create: %v", err)
	}
	if fake.creates != 1 {
		t.Fatalf("created %d servers, want 1", fake.creates)
	}
}

func TestServerCreateFailedBuild(t *testing.T) {
	fake := newFakeServerService()
	fake.failBuild = true
	r := &serverResource{clients: newTestClients(t, fake, nil)}
	m := testServerModel()
	err := r.doCreate(context.Background(), m)
	if err == nil {
		t.Fatal("want an error for a FAILED server")
	}
	// The id is kept so the tainted Server is destroyed by the next apply.
	if m.ID.ValueString() != "srv-1" {
		t.Fatalf("id lost: %+v", m.ID)
	}
}

func TestServerReadDeletedStateIsGone(t *testing.T) {
	fake := newFakeServerService()
	fake.servers["srv-9"] = &phronv1.Server{Id: "srv-9", State: phronv1.ServerState_SERVER_STATE_DELETED}
	r := &serverResource{clients: newTestClients(t, fake, nil)}
	m := &serverModel{ID: types.StringValue("srv-9")}
	gone, err := r.doRead(context.Background(), m)
	if err != nil || !gone {
		t.Fatalf("gone=%v err=%v", gone, err)
	}
}

func TestTierMapping(t *testing.T) {
	if _, err := tierToProto("gold"); err == nil {
		t.Fatal("want error for unknown tier")
	}
	if got, _ := tierToProto("dedicated"); got != phronv1.ServerTier_SERVER_TIER_DEDICATED {
		t.Fatalf("got %v", got)
	}
}
