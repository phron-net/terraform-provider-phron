package provider

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/proto"

	phronv1 "github.com/phron-net/api/gen/zonhoren/phron/v1"
)

var (
	_ resource.Resource              = (*storageBucketResource)(nil)
	_ resource.ResourceWithConfigure = (*storageBucketResource)(nil)
)

// NewStorageBucketResource returns the phron_storage_bucket resource.
func NewStorageBucketResource() resource.Resource { return &storageBucketResource{} }

type storageBucketResource struct {
	clients *Clients
}

type storageBucketModel struct {
	ID                             types.String `tfsdk:"id"`
	Name                           types.String `tfsdk:"name"`
	FullName                       types.String `tfsdk:"full_name"`
	Location                       types.String `tfsdk:"location"`
	WorkspaceID                    types.String `tfsdk:"workspace_id"`
	ObjectLock                     types.Bool   `tfsdk:"object_lock"`
	ObjectLockDefaultRetentionDays types.Int64  `tfsdk:"object_lock_default_retention_days"`
	LimitBytes                     types.Int64  `tfsdk:"limit_bytes"`
}

func (r *storageBucketResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage_bucket"
}

func (r *storageBucketResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "An S3-compatible bucket in Phron Object Storage. The first bucket of a Workspace also creates its storage in the given Location (which needs a positive balance). Destroying the bucket needs it to be empty.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The bucket's full name.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "The bucket name. In a shared Cell this is only the part after the Cell's bucket name prefix (at most 52 characters); full_name carries the whole name.",
				PlanModifiers: replace,
			},
			"full_name": schema.StringAttribute{
				Computed:      true,
				Description:   "The complete bucket name as S3 tools use it (the Cell's prefix plus name).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"location": schema.StringAttribute{
				Optional:      true,
				Description:   "The Location id of the storage. Required when creating the Workspace's first storage while more than one Location is offered, and when the Workspace has storage in two Locations.",
				PlanModifiers: replace,
			},
			"workspace_id": schema.StringAttribute{
				Optional:      true,
				Description:   "The Workspace the bucket belongs to. Empty means the Organization's default Workspace.",
				PlanModifiers: replace,
			},
			"object_lock": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				Description:   "Make the bucket Object Lock enabled (Governance mode). It can only be set when the bucket is created; changing it replaces the bucket.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"object_lock_default_retention_days": schema.Int64Attribute{
				Optional:    true,
				Description: "Default retention in days for a lock-enabled bucket. Unset: none.",
			},
			"limit_bytes": schema.Int64Attribute{
				Optional:    true,
				Description: "Optional size limit in bytes, a spend guard. Do not combine with a phron_storage_bucket_limit resource for the same bucket.",
			},
		},
	}
}

func (r *storageBucketResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, err := clientsFrom(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	r.clients = c
}

func (m *storageBucketModel) createRequest() *phronv1.CreateStorageBucketRequest {
	req := &phronv1.CreateStorageBucketRequest{
		Name:        m.Name.ValueString(),
		Location:    m.Location.ValueString(),
		WorkspaceId: m.WorkspaceID.ValueString(),
	}
	if !m.LimitBytes.IsNull() && !m.LimitBytes.IsUnknown() {
		req.LimitBytes = proto.Int64(m.LimitBytes.ValueInt64())
	}
	if m.ObjectLock.ValueBool() {
		lock := &phronv1.StorageObjectLock{}
		if !m.ObjectLockDefaultRetentionDays.IsNull() && !m.ObjectLockDefaultRetentionDays.IsUnknown() {
			lock.DefaultRetentionDays = proto.Int32(int32(m.ObjectLockDefaultRetentionDays.ValueInt64()))
		}
		req.ObjectLock = lock
	}
	return req
}

// doCreate creates the bucket. When the Organization has no storage yet the
// API prepares it first and answers with no bucket; the same call is repeated
// (it is idempotent) until the bucket exists.
func (r *storageBucketResource) doCreate(ctx context.Context, m *storageBucketModel) error {
	if m.ObjectLockDefaultRetentionDays.ValueInt64() > 0 && !m.ObjectLock.ValueBool() {
		return fmt.Errorf("object_lock_default_retention_days needs object_lock = true")
	}
	req := m.createRequest()
	var bucket *phronv1.StorageBucketItem
	err := pollUntil(ctx, "the storage to be ready for the bucket", storageTimeout, func(ctx context.Context) (bool, error) {
		res, err := r.clients.Storage.CreateStorageBucket(ctx, connect.NewRequest(req))
		if err != nil {
			return false, err
		}
		if res.Msg.GetState() == phronv1.StorageTenantState_STORAGE_TENANT_STATE_FAILED {
			return false, fmt.Errorf("the storage could not be prepared")
		}
		bucket = res.Msg.GetBucket()
		return bucket != nil, nil
	})
	if err != nil {
		return err
	}
	m.FullName = types.StringValue(bucket.GetName())
	m.ID = types.StringValue(bucket.GetName())
	return nil
}

func (r *storageBucketResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m storageBucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.doCreate(ctx, &m); err != nil {
		addErr(&resp.Diagnostics, "Creating the bucket failed", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// findBucket lists the buckets of the storage the model names and returns the
// one called fullName, or nil.
func findBucket(ctx context.Context, c *Clients, workspaceID, location, fullName string) (*phronv1.StorageBucketItem, error) {
	res, err := c.Storage.ListStorageBuckets(ctx, connect.NewRequest(&phronv1.ListStorageBucketsRequest{
		WorkspaceId: workspaceID,
		Location:    location,
	}))
	if err != nil {
		return nil, err
	}
	for _, b := range res.Msg.GetBuckets() {
		if b.GetName() == fullName {
			return b, nil
		}
	}
	return nil, nil
}

// doRead refreshes the model; gone=true when the bucket (or the storage) does
// not exist any more.
func (r *storageBucketResource) doRead(ctx context.Context, m *storageBucketModel) (gone bool, err error) {
	b, err := findBucket(ctx, r.clients, m.WorkspaceID.ValueString(), m.Location.ValueString(), m.FullName.ValueString())
	if isNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if b == nil {
		return true, nil
	}
	m.ID = types.StringValue(b.GetName())
	m.FullName = types.StringValue(b.GetName())
	if b.LimitBytes != nil {
		m.LimitBytes = types.Int64Value(b.GetLimitBytes())
	} else {
		m.LimitBytes = types.Int64Null()
	}
	m.ObjectLock = types.BoolValue(b.GetObjectLock() != nil)
	if lock := b.GetObjectLock(); lock != nil && lock.DefaultRetentionDays != nil {
		m.ObjectLockDefaultRetentionDays = types.Int64Value(int64(lock.GetDefaultRetentionDays()))
	} else {
		m.ObjectLockDefaultRetentionDays = types.Int64Null()
	}
	return false, nil
}

func (r *storageBucketResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m storageBucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	gone, err := r.doRead(ctx, &m)
	if err != nil {
		addErr(&resp.Diagnostics, "Reading the bucket failed", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// doUpdate applies the two things that can change in place: the default
// retention (the create call, repeated, sets it) and the size limit.
func (r *storageBucketResource) doUpdate(ctx context.Context, plan, prior *storageBucketModel) error {
	plan.ID, plan.FullName = prior.ID, prior.FullName
	if !plan.ObjectLockDefaultRetentionDays.Equal(prior.ObjectLockDefaultRetentionDays) {
		if _, err := r.clients.Storage.CreateStorageBucket(ctx, connect.NewRequest(plan.createRequest())); err != nil {
			return err
		}
	}
	if !plan.LimitBytes.Equal(prior.LimitBytes) {
		full, ws, loc := plan.FullName.ValueString(), plan.WorkspaceID.ValueString(), plan.Location.ValueString()
		if plan.LimitBytes.IsNull() {
			_, err := r.clients.Storage.ClearStorageBucketLimit(ctx, connect.NewRequest(&phronv1.ClearStorageBucketLimitRequest{
				BucketName: full, WorkspaceId: ws, Location: loc,
			}))
			return err
		}
		_, err := r.clients.Storage.SetStorageBucketLimit(ctx, connect.NewRequest(&phronv1.SetStorageBucketLimitRequest{
			BucketName: full, LimitBytes: plan.LimitBytes.ValueInt64(), WorkspaceId: ws, Location: loc,
		}))
		return err
	}
	return nil
}

func (r *storageBucketResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior storageBucketModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.doUpdate(ctx, &plan, &prior); err != nil {
		addErr(&resp.Diagnostics, "Updating the bucket failed", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *storageBucketResource) doDelete(ctx context.Context, m *storageBucketModel) error {
	_, err := r.clients.Storage.DeleteStorageBucket(ctx, connect.NewRequest(&phronv1.DeleteStorageBucketRequest{
		Name:        m.FullName.ValueString(),
		WorkspaceId: m.WorkspaceID.ValueString(),
		Location:    m.Location.ValueString(),
	}))
	if err != nil && !isNotFound(err) {
		return err
	}
	return nil
}

func (r *storageBucketResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m storageBucketModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.doDelete(ctx, &m); err != nil {
		addErr(&resp.Diagnostics, "Deleting the bucket failed", err)
	}
}
