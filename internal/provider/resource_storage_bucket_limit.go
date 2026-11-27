package provider

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	phronv1 "github.com/phron-net/api/gen/zonhoren/phron/v1"
)

var (
	_ resource.Resource              = (*storageBucketLimitResource)(nil)
	_ resource.ResourceWithConfigure = (*storageBucketLimitResource)(nil)
)

// NewStorageBucketLimitResource returns the phron_storage_bucket_limit resource.
func NewStorageBucketLimitResource() resource.Resource { return &storageBucketLimitResource{} }

type storageBucketLimitResource struct {
	clients *Clients
}

type storageBucketLimitModel struct {
	ID          types.String `tfsdk:"id"`
	BucketName  types.String `tfsdk:"bucket_name"`
	LimitBytes  types.Int64  `tfsdk:"limit_bytes"`
	Location    types.String `tfsdk:"location"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Phase       types.String `tfsdk:"phase"`
}

func (r *storageBucketLimitResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage_bucket_limit"
}

func (r *storageBucketLimitResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "The optional size limit of an existing bucket, a spend guard. Destroying the resource removes the limit. Do not set limit_bytes on the phron_storage_bucket as well.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The bucket's full name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"bucket_name": schema.StringAttribute{
				Required:      true,
				Description:   "The bucket's full name (phron_storage_bucket.full_name).",
				PlanModifiers: replace,
			},
			"limit_bytes": schema.Int64Attribute{
				Required:    true,
				Description: "The size limit in bytes.",
			},
			"location": schema.StringAttribute{
				Optional:      true,
				Description:   "The Location id of the storage, when the Workspace has storage in more than one.",
				PlanModifiers: replace,
			},
			"workspace_id": schema.StringAttribute{
				Optional:      true,
				Description:   "The Workspace of the bucket. Empty means the Organization's default Workspace.",
				PlanModifiers: replace,
			},
			"phase": schema.StringAttribute{
				Computed:    true,
				Description: "How far the limit has reached the storage servers: applying, applied, not_converged or not_verifiable.",
			},
		},
	}
}

func (r *storageBucketLimitResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, err := clientsFrom(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	r.clients = c
}

func phaseWord(b *phronv1.StorageBucketItem) types.String {
	st := b.GetLimitState()
	if st == nil {
		return types.StringValue("")
	}
	return types.StringValue(enumWord("STORAGE_LIMIT_PHASE_", st.GetPhase().String()))
}

func (r *storageBucketLimitResource) doSet(ctx context.Context, m *storageBucketLimitModel) error {
	res, err := r.clients.Storage.SetStorageBucketLimit(ctx, connect.NewRequest(&phronv1.SetStorageBucketLimitRequest{
		BucketName:  m.BucketName.ValueString(),
		LimitBytes:  m.LimitBytes.ValueInt64(),
		WorkspaceId: m.WorkspaceID.ValueString(),
		Location:    m.Location.ValueString(),
	}))
	if err != nil {
		return err
	}
	m.ID = m.BucketName
	m.Phase = phaseWord(res.Msg.GetBucket())
	return nil
}

func (r *storageBucketLimitResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m storageBucketLimitModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.doSet(ctx, &m); err != nil {
		addErr(&resp.Diagnostics, "Setting the bucket limit failed", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *storageBucketLimitResource) doRead(ctx context.Context, m *storageBucketLimitModel) (gone bool, err error) {
	b, err := findBucket(ctx, r.clients, m.WorkspaceID.ValueString(), m.Location.ValueString(), m.BucketName.ValueString())
	if isNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if b == nil || b.LimitBytes == nil {
		return true, nil
	}
	m.LimitBytes = types.Int64Value(b.GetLimitBytes())
	m.Phase = phaseWord(b)
	return false, nil
}

func (r *storageBucketLimitResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m storageBucketLimitModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	gone, err := r.doRead(ctx, &m)
	if err != nil {
		addErr(&resp.Diagnostics, "Reading the bucket limit failed", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *storageBucketLimitResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m storageBucketLimitModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.doSet(ctx, &m); err != nil {
		addErr(&resp.Diagnostics, "Updating the bucket limit failed", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *storageBucketLimitResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m storageBucketLimitModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.clients.Storage.ClearStorageBucketLimit(ctx, connect.NewRequest(&phronv1.ClearStorageBucketLimitRequest{
		BucketName:  m.BucketName.ValueString(),
		WorkspaceId: m.WorkspaceID.ValueString(),
		Location:    m.Location.ValueString(),
	}))
	if err != nil && !isNotFound(err) {
		addErr(&resp.Diagnostics, fmt.Sprintf("Removing the limit of bucket %s failed", m.BucketName.ValueString()), err)
	}
}
