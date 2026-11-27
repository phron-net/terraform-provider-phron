package provider

import (
	"context"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/proto"

	phronv1 "github.com/phron-net/api/gen/zonhoren/phron/v1"
	"github.com/phron-net/api/gen/zonhoren/phron/v1/phronv1connect"
)

type fakeStorageService struct {
	phronv1connect.UnimplementedStorageServiceHandler

	mu          sync.Mutex
	tenantReady bool
	buckets     map[string]*phronv1.StorageBucketItem
	keys        map[string]*phronv1.StorageKeyItem
	createCalls int
}

func newFakeStorageService() *fakeStorageService {
	return &fakeStorageService{buckets: map[string]*phronv1.StorageBucketItem{}, keys: map[string]*phronv1.StorageKeyItem{}}
}

const fakePrefix = "t1-"

func (f *fakeStorageService) CreateStorageBucket(_ context.Context, req *connect.Request[phronv1.CreateStorageBucketRequest]) (*connect.Response[phronv1.CreateStorageBucketResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++
	if !f.tenantReady {
		// The first call prepares the storage and answers with no bucket.
		f.tenantReady = true
		return connect.NewResponse(&phronv1.CreateStorageBucketResponse{State: phronv1.StorageTenantState_STORAGE_TENANT_STATE_PROVISIONING}), nil
	}
	m := req.Msg
	name := fakePrefix + m.GetName()
	b, ok := f.buckets[name]
	if !ok {
		b = &phronv1.StorageBucketItem{Name: name}
		f.buckets[name] = b
	}
	b.LimitBytes = m.LimitBytes
	b.ObjectLock = m.GetObjectLock()
	return connect.NewResponse(&phronv1.CreateStorageBucketResponse{Bucket: b, State: phronv1.StorageTenantState_STORAGE_TENANT_STATE_ACTIVE}), nil
}

func (f *fakeStorageService) ListStorageBuckets(_ context.Context, _ *connect.Request[phronv1.ListStorageBucketsRequest]) (*connect.Response[phronv1.ListStorageBucketsResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &phronv1.ListStorageBucketsResponse{}
	for _, b := range f.buckets {
		out.Buckets = append(out.Buckets, b)
	}
	return connect.NewResponse(out), nil
}

func (f *fakeStorageService) DeleteStorageBucket(_ context.Context, req *connect.Request[phronv1.DeleteStorageBucketRequest]) (*connect.Response[phronv1.DeleteStorageBucketResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.buckets[req.Msg.GetName()]; !ok {
		return nil, connect.NewError(connect.CodeNotFound, errFake)
	}
	delete(f.buckets, req.Msg.GetName())
	return connect.NewResponse(&phronv1.DeleteStorageBucketResponse{Name: req.Msg.GetName()}), nil
}

func (f *fakeStorageService) SetStorageBucketLimit(_ context.Context, req *connect.Request[phronv1.SetStorageBucketLimitRequest]) (*connect.Response[phronv1.SetStorageBucketLimitResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.buckets[req.Msg.GetBucketName()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errFake)
	}
	b.LimitBytes = proto.Int64(req.Msg.GetLimitBytes())
	b.LimitState = &phronv1.StorageLimitState{Phase: phronv1.StorageLimitPhase_STORAGE_LIMIT_PHASE_APPLIED}
	return connect.NewResponse(&phronv1.SetStorageBucketLimitResponse{Bucket: b}), nil
}

func (f *fakeStorageService) ClearStorageBucketLimit(_ context.Context, req *connect.Request[phronv1.ClearStorageBucketLimitRequest]) (*connect.Response[phronv1.ClearStorageBucketLimitResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.buckets[req.Msg.GetBucketName()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, errFake)
	}
	b.LimitBytes = nil
	return connect.NewResponse(&phronv1.ClearStorageBucketLimitResponse{Bucket: b}), nil
}

func (f *fakeStorageService) CreateStorageKey(_ context.Context, req *connect.Request[phronv1.CreateStorageKeyRequest]) (*connect.Response[phronv1.CreateStorageKeyResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := &phronv1.StorageKeyItem{
		Id: "key-1", Name: req.Msg.GetName(), AccessKeyId: "AK123", Preset: req.Msg.GetPreset(),
		BucketNames: req.Msg.GetBucketNames(), Status: phronv1.StorageKeyStatus_STORAGE_KEY_STATUS_ACTIVE, CreatedBy: "user-1",
	}
	f.keys[k.Id] = k
	return connect.NewResponse(&phronv1.CreateStorageKeyResponse{Key: k, SecretAccessKey: "s3cret"}), nil
}

func (f *fakeStorageService) ListStorageKeys(_ context.Context, _ *connect.Request[phronv1.ListStorageKeysRequest]) (*connect.Response[phronv1.ListStorageKeysResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &phronv1.ListStorageKeysResponse{}
	for _, k := range f.keys {
		out.Keys = append(out.Keys, k)
	}
	return connect.NewResponse(out), nil
}

func (f *fakeStorageService) DeleteStorageKey(_ context.Context, req *connect.Request[phronv1.DeleteStorageKeyRequest]) (*connect.Response[phronv1.DeleteStorageKeyResponse], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.keys, req.Msg.GetKeyId())
	return connect.NewResponse(&phronv1.DeleteStorageKeyResponse{KeyId: req.Msg.GetKeyId()}), nil
}

func testBucketModel() *storageBucketModel {
	return &storageBucketModel{
		ID:                             types.StringUnknown(),
		Name:                           types.StringValue("logs"),
		FullName:                       types.StringUnknown(),
		Location:                       types.StringValue("fsn"),
		WorkspaceID:                    types.StringNull(),
		ObjectLock:                     types.BoolValue(true),
		ObjectLockDefaultRetentionDays: types.Int64Value(30),
		LimitBytes:                     types.Int64Value(1000),
	}
}

func TestBucketLifecycle(t *testing.T) {
	ctx := context.Background()
	fake := newFakeStorageService()
	r := &storageBucketResource{clients: newTestClients(t, nil, fake)}

	m := testBucketModel()
	if err := r.doCreate(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}
	if fake.createCalls != 2 {
		t.Fatalf("want a repeated create after provisioning, got %d calls", fake.createCalls)
	}
	if m.FullName.ValueString() != "t1-logs" || m.ID.ValueString() != "t1-logs" {
		t.Fatalf("names: %+v", m)
	}
	b := fake.buckets["t1-logs"]
	if b.GetLimitBytes() != 1000 || b.GetObjectLock().GetDefaultRetentionDays() != 30 {
		t.Fatalf("create request not mapped: %+v", b)
	}

	read := *m
	read.LimitBytes, read.ObjectLock = types.Int64Value(1), types.BoolValue(false)
	gone, err := r.doRead(ctx, &read)
	if err != nil || gone {
		t.Fatalf("read: gone=%v err=%v", gone, err)
	}
	if read.LimitBytes.ValueInt64() != 1000 || !read.ObjectLock.ValueBool() || read.ObjectLockDefaultRetentionDays.ValueInt64() != 30 {
		t.Fatalf("read mapped wrongly: %+v", read)
	}

	// A bucket that does not manage the limit (it comes from a
	// phron_storage_bucket_limit resource) keeps limit_bytes null.
	unmanaged := *m
	unmanaged.LimitBytes = types.Int64Null()
	if _, err := r.doRead(ctx, &unmanaged); err != nil || !unmanaged.LimitBytes.IsNull() {
		t.Fatalf("an unmanaged limit must stay null: err=%v %+v", err, unmanaged.LimitBytes)
	}

	// Change the limit, then remove it.
	plan := *m
	plan.LimitBytes = types.Int64Value(2000)
	if err := r.doUpdate(ctx, &plan, m); err != nil {
		t.Fatalf("update: %v", err)
	}
	if fake.buckets["t1-logs"].GetLimitBytes() != 2000 {
		t.Fatalf("limit not set")
	}
	prior := plan
	plan.LimitBytes = types.Int64Null()
	if err := r.doUpdate(ctx, &plan, &prior); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if fake.buckets["t1-logs"].LimitBytes != nil {
		t.Fatalf("limit not cleared")
	}

	if err := r.doDelete(ctx, m); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := r.doDelete(ctx, m); err != nil {
		t.Fatalf("second delete must not fail: %v", err)
	}
	gone, err = r.doRead(ctx, &read)
	if err != nil || !gone {
		t.Fatalf("read after delete: gone=%v err=%v", gone, err)
	}
}

func TestBucketRetentionNeedsLock(t *testing.T) {
	r := &storageBucketResource{clients: newTestClients(t, nil, newFakeStorageService())}
	m := testBucketModel()
	m.ObjectLock = types.BoolValue(false)
	if err := r.doCreate(context.Background(), m); err == nil {
		t.Fatal("want an error")
	}
}

func TestBucketLimitResource(t *testing.T) {
	ctx := context.Background()
	fake := newFakeStorageService()
	fake.buckets["t1-data"] = &phronv1.StorageBucketItem{Name: "t1-data"}
	r := &storageBucketLimitResource{clients: newTestClients(t, nil, fake)}

	m := &storageBucketLimitModel{
		ID: types.StringUnknown(), BucketName: types.StringValue("t1-data"), LimitBytes: types.Int64Value(500),
		Location: types.StringNull(), WorkspaceID: types.StringNull(), Phase: types.StringUnknown(),
	}
	if err := r.doSet(ctx, m); err != nil {
		t.Fatalf("set: %v", err)
	}
	if m.Phase.ValueString() != "applied" || m.ID.ValueString() != "t1-data" {
		t.Fatalf("set result: %+v", m)
	}
	gone, err := r.doRead(ctx, m)
	if err != nil || gone {
		t.Fatalf("read: gone=%v err=%v", gone, err)
	}
	fake.buckets["t1-data"].LimitBytes = nil // cleared outside Terraform
	gone, err = r.doRead(ctx, m)
	if err != nil || !gone {
		t.Fatalf("read without limit: gone=%v err=%v", gone, err)
	}
}

func TestStorageKeyKeepsSecret(t *testing.T) {
	ctx := context.Background()
	fake := newFakeStorageService()
	r := &storageKeyResource{clients: newTestClients(t, nil, fake)}
	m := &storageKeyModel{
		ID: types.StringUnknown(), Name: types.StringValue("ci"), Preset: types.StringValue("read_only_objects"),
		BucketNames: stringsToSet([]string{"t1-data"}), ExpiresAt: types.StringNull(),
		Location: types.StringNull(), WorkspaceID: types.StringNull(),
		AccessKeyID: types.StringUnknown(), SecretAccessKey: types.StringUnknown(), Status: types.StringUnknown(), CreatedBy: types.StringUnknown(),
	}
	if err := r.doCreate(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}
	if m.SecretAccessKey.ValueString() != "s3cret" || m.AccessKeyID.ValueString() != "AK123" {
		t.Fatalf("create result: %+v", m)
	}
	if fake.keys["key-1"].GetPreset() != phronv1.StoragePermissionPreset_STORAGE_PERMISSION_PRESET_READ_ONLY_OBJECTS {
		t.Fatal("preset not mapped")
	}
	gone, err := r.doRead(ctx, m)
	if err != nil || gone || m.SecretAccessKey.ValueString() != "s3cret" {
		t.Fatalf("read: gone=%v err=%v secret=%q", gone, err, m.SecretAccessKey.ValueString())
	}
	delete(fake.keys, "key-1")
	gone, _ = r.doRead(ctx, m)
	if !gone {
		t.Fatal("a deleted key must be reported gone")
	}
}

func TestPresetMapping(t *testing.T) {
	if _, err := presetToProto("admin"); err == nil {
		t.Fatal("want error")
	}
}
