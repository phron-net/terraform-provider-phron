package provider

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"google.golang.org/protobuf/types/known/timestamppb"

	phronv1 "github.com/phron-net/api/gen/zonhoren/phron/v1"
)

var (
	_ resource.Resource              = (*storageKeyResource)(nil)
	_ resource.ResourceWithConfigure = (*storageKeyResource)(nil)
)

// NewStorageKeyResource returns the phron_storage_key resource.
func NewStorageKeyResource() resource.Resource { return &storageKeyResource{} }

type storageKeyResource struct {
	clients *Clients
}

type storageKeyModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Preset          types.String `tfsdk:"preset"`
	BucketNames     types.Set    `tfsdk:"bucket_names"`
	ExpiresAt       types.String `tfsdk:"expires_at"`
	Location        types.String `tfsdk:"location"`
	WorkspaceID     types.String `tfsdk:"workspace_id"`
	AccessKeyID     types.String `tfsdk:"access_key_id"`
	SecretAccessKey types.String `tfsdk:"secret_access_key"`
	Status          types.String `tfsdk:"status"`
	CreatedBy       types.String `tfsdk:"created_by"`
}

func (r *storageKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage_key"
}

func (r *storageKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A Storage Access Key for the S3 endpoint. The secret is returned by the API once, at creation, and is stored in the Terraform state: protect the state accordingly. Every attribute forces a new key.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The key id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "A name for the key.",
				PlanModifiers: replace,
			},
			"preset": schema.StringAttribute{
				Required:      true,
				Description:   "The permission preset: \"full\", \"read_write_objects\" or \"read_only_objects\".",
				PlanModifiers: replace,
			},
			"bucket_names": schema.SetAttribute{
				Optional:      true,
				ElementType:   types.StringType,
				Description:   "Full names of the buckets the key may reach. Unset: every bucket.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
			},
			"expires_at": schema.StringAttribute{
				Optional:      true,
				Description:   "When the key expires, as an RFC 3339 timestamp. Unset: never.",
				PlanModifiers: replace,
			},
			"location": schema.StringAttribute{
				Optional:      true,
				Description:   "The Location id of the storage the key is for, when the Workspace has storage in more than one.",
				PlanModifiers: replace,
			},
			"workspace_id": schema.StringAttribute{
				Optional:      true,
				Description:   "The Workspace of the storage. Empty means the Organization's default Workspace.",
				PlanModifiers: replace,
			},
			"access_key_id": schema.StringAttribute{
				Computed:      true,
				Description:   "The S3 access key id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"secret_access_key": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "The S3 secret access key. Readable only at creation; kept in the state.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "active, suspended or expired.",
			},
			"created_by": schema.StringAttribute{
				Computed:      true,
				Description:   "The member who created the key.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *storageKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, err := clientsFrom(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	r.clients = c
}

func presetToProto(s string) (phronv1.StoragePermissionPreset, error) {
	switch s {
	case "full":
		return phronv1.StoragePermissionPreset_STORAGE_PERMISSION_PRESET_FULL, nil
	case "read_write_objects":
		return phronv1.StoragePermissionPreset_STORAGE_PERMISSION_PRESET_READ_WRITE_OBJECTS, nil
	case "read_only_objects":
		return phronv1.StoragePermissionPreset_STORAGE_PERMISSION_PRESET_READ_ONLY_OBJECTS, nil
	}
	return phronv1.StoragePermissionPreset_STORAGE_PERMISSION_PRESET_UNSPECIFIED,
		fmt.Errorf("preset must be \"full\", \"read_write_objects\" or \"read_only_objects\", got %q", s)
}

func (r *storageKeyResource) doCreate(ctx context.Context, m *storageKeyModel) error {
	preset, err := presetToProto(m.Preset.ValueString())
	if err != nil {
		return err
	}
	buckets, err := setToStrings(ctx, m.BucketNames)
	if err != nil {
		return err
	}
	req := &phronv1.CreateStorageKeyRequest{
		Name:        m.Name.ValueString(),
		Preset:      preset,
		BucketNames: buckets,
		WorkspaceId: m.WorkspaceID.ValueString(),
		Location:    m.Location.ValueString(),
	}
	if !m.ExpiresAt.IsNull() && !m.ExpiresAt.IsUnknown() {
		t, err := time.Parse(time.RFC3339, m.ExpiresAt.ValueString())
		if err != nil {
			return fmt.Errorf("expires_at must be an RFC 3339 timestamp: %w", err)
		}
		req.ExpiresAt = timestamppb.New(t)
	}
	res, err := r.clients.Storage.CreateStorageKey(ctx, connect.NewRequest(req))
	if err != nil {
		return err
	}
	k := res.Msg.GetKey()
	m.ID = types.StringValue(k.GetId())
	m.AccessKeyID = types.StringValue(k.GetAccessKeyId())
	m.SecretAccessKey = types.StringValue(res.Msg.GetSecretAccessKey())
	m.Status = types.StringValue(enumWord("STORAGE_KEY_STATUS_", k.GetStatus().String()))
	m.CreatedBy = types.StringValue(k.GetCreatedBy())
	return nil
}

func (r *storageKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m storageKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.doCreate(ctx, &m); err != nil {
		addErr(&resp.Diagnostics, "Creating the Storage Access Key failed", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// doRead finds the key in the list; the secret is never returned again, so
// the state's value stays.
func (r *storageKeyResource) doRead(ctx context.Context, m *storageKeyModel) (gone bool, err error) {
	res, err := r.clients.Storage.ListStorageKeys(ctx, connect.NewRequest(&phronv1.ListStorageKeysRequest{
		WorkspaceId: m.WorkspaceID.ValueString(),
		Location:    m.Location.ValueString(),
	}))
	if isNotFound(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, k := range res.Msg.GetKeys() {
		if k.GetId() != m.ID.ValueString() {
			continue
		}
		m.Name = types.StringValue(k.GetName())
		m.AccessKeyID = types.StringValue(k.GetAccessKeyId())
		m.Status = types.StringValue(enumWord("STORAGE_KEY_STATUS_", k.GetStatus().String()))
		m.CreatedBy = types.StringValue(k.GetCreatedBy())
		if len(k.GetBucketNames()) > 0 {
			m.BucketNames = stringsToSet(k.GetBucketNames())
		}
		if k.GetExpiresAt() != nil && !m.ExpiresAt.IsNull() {
			// Keep the configured text when it names the same instant.
			if t, perr := time.Parse(time.RFC3339, m.ExpiresAt.ValueString()); perr != nil || !t.Equal(k.GetExpiresAt().AsTime()) {
				m.ExpiresAt = types.StringValue(k.GetExpiresAt().AsTime().UTC().Format(time.RFC3339))
			}
		}
		return false, nil
	}
	return true, nil
}

func (r *storageKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m storageKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	gone, err := r.doRead(ctx, &m)
	if err != nil {
		addErr(&resp.Diagnostics, "Reading the Storage Access Key failed", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Update is never needed: every configurable attribute forces a new key.
func (r *storageKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m storageKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *storageKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m storageKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.clients.Storage.DeleteStorageKey(ctx, connect.NewRequest(&phronv1.DeleteStorageKeyRequest{
		KeyId:       m.ID.ValueString(),
		WorkspaceId: m.WorkspaceID.ValueString(),
	}))
	if err != nil && !isNotFound(err) {
		addErr(&resp.Diagnostics, "Deleting the Storage Access Key failed", err)
	}
}
