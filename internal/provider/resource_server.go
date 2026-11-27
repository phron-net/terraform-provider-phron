package provider

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	phronv1 "github.com/phron-net/api/gen/zonhoren/phron/v1"
)

var (
	_ resource.Resource                = (*serverResource)(nil)
	_ resource.ResourceWithConfigure   = (*serverResource)(nil)
	_ resource.ResourceWithImportState = (*serverResource)(nil)
)

// NewServerResource returns the phron_server resource.
func NewServerResource() resource.Resource { return &serverResource{} }

type serverResource struct {
	clients *Clients
}

type serverModel struct {
	ID                  types.String `tfsdk:"id"`
	Name                types.String `tfsdk:"name"`
	OperatingSystemID   types.String `tfsdk:"operating_system_id"`
	SSHKeyIDs           types.Set    `tfsdk:"ssh_key_ids"`
	Location            types.String `tfsdk:"location"`
	WorkspaceID         types.String `tfsdk:"workspace_id"`
	Tier                types.String `tfsdk:"tier"`
	VCPUs               types.Int64  `tfsdk:"vcpus"`
	MemoryGiB           types.Int64  `tfsdk:"memory_gib"`
	DiskGiB             types.Int64  `tfsdk:"disk_gib"`
	UserData            types.String `tfsdk:"user_data"`
	RestoreFromBackupID types.String `tfsdk:"restore_from_backup_id"`
	State               types.String `tfsdk:"state"`
	Power               types.String `tfsdk:"power"`
	IPv6Prefix          types.String `tfsdk:"ipv6_prefix"`
	SSHAddress          types.String `tfsdk:"ssh_address"`
}

func (r *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *serverResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A Phron Server: a virtual server with its own public IPv6 /64, billed hourly. Creating it waits until the Server is running; destroying it deletes the Server, its disk and its backups for good.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The Server id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "The Server's name, 1 to 63 characters. Changing it replaces the Server (the API has no rename).",
				PlanModifiers: replace,
			},
			"operating_system_id": schema.StringAttribute{
				Required:      true,
				Description:   "The Operating System id; see the phron_operating_systems data source.",
				PlanModifiers: replace,
			},
			"ssh_key_ids": schema.SetAttribute{
				Required:      true,
				ElementType:   types.StringType,
				Description:   "Ids of saved SSH keys (phron_ssh_key), installed at first boot. At least one is required.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
			},
			"location": schema.StringAttribute{
				Required:      true,
				Description:   "The Location id the Server lives in for good; see the phron_locations data source.",
				PlanModifiers: replace,
			},
			"workspace_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "The Workspace the Server belongs to for good. Empty means the Organization's default Workspace.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"tier": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Default:       stringdefault.StaticString("shared"),
				Description:   "\"shared\" (default) or \"dedicated\" vCPUs. A Server keeps its tier: changing it replaces the Server.",
				PlanModifiers: replace,
			},
			"vcpus": schema.Int64Attribute{
				Required:    true,
				Description: "Number of vCPUs. Changing the size resizes the Server in place (the disk can grow, never shrink).",
			},
			"memory_gib": schema.Int64Attribute{
				Required:    true,
				Description: "Memory in GiB.",
			},
			"disk_gib": schema.Int64Attribute{
				Required:    true,
				Description: "Disk in GiB.",
			},
			"user_data": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "Cloud-init user data run once at first boot, after the platform's own part: plain text of at most 65536 bytes whose first line is `#cloud-config`, `#!`, `#cloud-boothook` or `#include`. " +
					"Never returned by the API, so it is kept from your configuration. Not a place for secrets: anyone who can read the guest's disk can read it. Changing it replaces the Server (cloud-init runs once per instance).",
				PlanModifiers: replace,
			},
			"restore_from_backup_id": schema.StringAttribute{
				Optional:      true,
				Description:   "Start the Server from a Backup or Snapshot of a deleted Server of the same Organization. The Operating System of that Server is used and operating_system_id is ignored by the API.",
				PlanModifiers: replace,
			},
			"state": schema.StringAttribute{
				Computed:    true,
				Description: "The lifecycle state, for example \"running\" or \"stopped\".",
			},
			"power": schema.StringAttribute{
				Computed:    true,
				Description: "The observed power state, for example \"running\".",
			},
			"ipv6_prefix": schema.StringAttribute{
				Computed:      true,
				Description:   "The Server's public IPv6 prefix.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"ssh_address": schema.StringAttribute{
				Computed:      true,
				Description:   "The address to ssh to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *serverResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, err := clientsFrom(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	r.clients = c
}

func tierToProto(s string) (phronv1.ServerTier, error) {
	switch strings.ToLower(s) {
	case "", "shared":
		return phronv1.ServerTier_SERVER_TIER_SHARED, nil
	case "dedicated":
		return phronv1.ServerTier_SERVER_TIER_DEDICATED, nil
	}
	return phronv1.ServerTier_SERVER_TIER_UNSPECIFIED, fmt.Errorf("tier must be \"shared\" or \"dedicated\", got %q", s)
}

func tierFromProto(t phronv1.ServerTier) string {
	if t == phronv1.ServerTier_SERVER_TIER_DEDICATED {
		return "dedicated"
	}
	return "shared"
}

func (m *serverModel) size() (*phronv1.ServerSize, error) {
	tier, err := tierToProto(m.Tier.ValueString())
	if err != nil {
		return nil, err
	}
	return &phronv1.ServerSize{
		Tier:      tier,
		Vcpus:     int32(m.VCPUs.ValueInt64()),
		MemoryGib: int32(m.MemoryGiB.ValueInt64()),
		DiskGib:   int32(m.DiskGiB.ValueInt64()),
	}, nil
}

// applyServer copies what the API reports into the model. The computed
// attributes are always set; with full, the configurable ones are taken from
// the API too (Read), otherwise the plan's values stay (Create and Update).
func applyServer(ctx context.Context, m *serverModel, s *phronv1.Server, full bool) {
	m.ID = types.StringValue(s.GetId())
	m.State = types.StringValue(enumWord("SERVER_STATE_", s.GetState().String()))
	m.Power = types.StringValue(enumWord("SERVER_POWER_", s.GetPower().String()))
	m.IPv6Prefix = types.StringValue(s.GetIpv6Prefix())
	m.SSHAddress = types.StringValue(s.GetSshAddress())
	m.WorkspaceID = types.StringValue(s.GetWorkspaceId())
	if !full {
		return
	}
	m.Name = types.StringValue(s.GetName())
	m.Location = types.StringValue(s.GetLocation())
	if os := s.GetOperatingSystem(); os != nil && os.GetId() != "" {
		m.OperatingSystemID = types.StringValue(os.GetId())
	}
	if sz := s.GetSize(); sz != nil {
		m.Tier = types.StringValue(tierFromProto(sz.GetTier()))
		m.VCPUs = types.Int64Value(int64(sz.GetVcpus()))
		m.MemoryGiB = types.Int64Value(int64(sz.GetMemoryGib()))
		m.DiskGiB = types.Int64Value(int64(sz.GetDiskGib()))
	}
	if keys := s.GetSshKeyIds(); len(keys) > 0 {
		m.SSHKeyIDs = stringsToSet(keys)
	}
}

// waitServer polls GetServer until done reports true. A DELETED or FAILED
// Server ends the wait with an error unless done accepts it first.
func (r *serverResource) waitServer(ctx context.Context, id, what string, done func(*phronv1.Server) bool) (*phronv1.Server, error) {
	var last *phronv1.Server
	err := pollUntil(ctx, what, serverTimeout, func(ctx context.Context) (bool, error) {
		res, err := r.clients.Server.GetServer(ctx, connect.NewRequest(&phronv1.GetServerRequest{ServerId: id}))
		if err != nil {
			return false, err
		}
		last = res.Msg.GetServer()
		if done(last) {
			return true, nil
		}
		switch last.GetState() {
		case phronv1.ServerState_SERVER_STATE_FAILED:
			return false, fmt.Errorf("server %s failed", id)
		case phronv1.ServerState_SERVER_STATE_DELETING, phronv1.ServerState_SERVER_STATE_DELETED:
			return false, fmt.Errorf("server %s is being deleted", id)
		}
		return false, nil
	})
	return last, err
}

func serverReady(s *phronv1.Server) bool {
	return s.GetState() == phronv1.ServerState_SERVER_STATE_RUNNING && !s.GetResizing() && !s.GetRestoring()
}

// doCreate creates the Server and waits until it runs. The model gets its id
// as soon as the API accepted the request, so a failed wait still leaves the
// Server in state (tainted) and the next apply replaces it.
func (r *serverResource) doCreate(ctx context.Context, m *serverModel) error {
	size, err := m.size()
	if err != nil {
		return err
	}
	keys, err := setToStrings(ctx, m.SSHKeyIDs)
	if err != nil {
		return err
	}
	requestID, err := newRequestID()
	if err != nil {
		return err
	}
	req := &phronv1.CreateServerRequest{
		Name:                m.Name.ValueString(),
		OperatingSystemId:   m.OperatingSystemID.ValueString(),
		SshKeyIds:           keys,
		RequestId:           requestID,
		Size:                size,
		RestoreFromBackupId: m.RestoreFromBackupID.ValueString(),
		Location:            m.Location.ValueString(),
		WorkspaceId:         m.WorkspaceID.ValueString(),
		UserData:            m.UserData.ValueString(),
	}
	var created *phronv1.Server
	err = retryUnavailable(ctx, func() error {
		res, err := r.clients.Server.CreateServer(ctx, connect.NewRequest(req))
		if err != nil {
			return err
		}
		created = res.Msg.GetServer()
		return nil
	})
	if err != nil {
		return err
	}
	applyServer(ctx, m, created, false)

	final, err := r.waitServer(ctx, created.GetId(), "the server to be running", serverReady)
	if final != nil {
		applyServer(ctx, m, final, false)
	}
	return err
}

func (r *serverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Unknown computed values must not reach state if the create fails early.
	m.ID, m.State, m.Power = types.StringUnknown(), types.StringUnknown(), types.StringUnknown()
	err := r.doCreate(ctx, &m)
	if err != nil {
		addErr(&resp.Diagnostics, "Creating the Server failed", err)
	}
	if !m.ID.IsUnknown() {
		resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	}
}

// doRead refreshes the model; it reports gone=true when the Server no longer
// exists (NotFound, or state DELETED).
func (r *serverResource) doRead(ctx context.Context, m *serverModel) (gone bool, err error) {
	res, err := r.clients.Server.GetServer(ctx, connect.NewRequest(&phronv1.GetServerRequest{ServerId: m.ID.ValueString()}))
	if isNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	s := res.Msg.GetServer()
	if s.GetState() == phronv1.ServerState_SERVER_STATE_DELETED {
		return true, nil
	}
	applyServer(ctx, m, s, true)
	return false, nil
}

func (r *serverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	gone, err := r.doRead(ctx, &m)
	if err != nil {
		addErr(&resp.Diagnostics, "Reading the Server failed", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// doUpdate resizes the Server. Everything else forces a replacement, so the
// size is the only thing that can differ between state and plan.
func (r *serverResource) doUpdate(ctx context.Context, plan *serverModel, prior *serverModel) error {
	size, err := plan.size()
	if err != nil {
		return err
	}
	id := prior.ID.ValueString()
	res, err := r.clients.Server.ResizeServer(ctx, connect.NewRequest(&phronv1.ResizeServerRequest{ServerId: id, Size: size}))
	if err != nil {
		return err
	}
	applyServer(ctx, plan, res.Msg.GetServer(), false)
	final, err := r.waitServer(ctx, id, "the resize to finish", func(s *phronv1.Server) bool {
		return !s.GetResizing() && (s.GetState() == phronv1.ServerState_SERVER_STATE_RUNNING || s.GetState() == phronv1.ServerState_SERVER_STATE_STOPPED)
	})
	if final != nil {
		applyServer(ctx, plan, final, false)
	}
	return err
}

func (r *serverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = prior.ID
	if err := r.doUpdate(ctx, &plan, &prior); err != nil {
		addErr(&resp.Diagnostics, "Resizing the Server failed", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// doDelete deletes the Server and waits until it is gone.
func (r *serverResource) doDelete(ctx context.Context, id string) error {
	_, err := r.clients.Server.DeleteServer(ctx, connect.NewRequest(&phronv1.DeleteServerRequest{ServerId: id}))
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return pollUntil(ctx, "the server to be deleted", serverTimeout, func(ctx context.Context) (bool, error) {
		res, err := r.clients.Server.GetServer(ctx, connect.NewRequest(&phronv1.GetServerRequest{ServerId: id}))
		if isNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return res.Msg.GetServer().GetState() == phronv1.ServerState_SERVER_STATE_DELETED, nil
	})
}

func (r *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.doDelete(ctx, m.ID.ValueString()); err != nil {
		addErr(&resp.Diagnostics, "Deleting the Server failed", err)
	}
}

func (r *serverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
