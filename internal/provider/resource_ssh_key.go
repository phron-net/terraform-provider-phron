package provider

import (
	"context"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	phronv1 "github.com/phron-net/api/gen/zonhoren/phron/v1"
)

var (
	_ resource.Resource                = (*sshKeyResource)(nil)
	_ resource.ResourceWithConfigure   = (*sshKeyResource)(nil)
	_ resource.ResourceWithImportState = (*sshKeyResource)(nil)
)

// NewSSHKeyResource returns the phron_ssh_key resource.
func NewSSHKeyResource() resource.Resource { return &sshKeyResource{} }

type sshKeyResource struct {
	clients *Clients
}

type sshKeyModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	Fingerprint types.String `tfsdk:"fingerprint"`
}

func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *sshKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A saved SSH public key of the Organization, which Servers install at first boot. Servers already created with a key keep it when the key is deleted.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "The SSH key id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "A name for the key. Changing it replaces the key.",
				PlanModifiers: replace,
			},
			"public_key": schema.StringAttribute{
				Required:      true,
				Description:   "The OpenSSH public key. Changing it replaces the key.",
				PlanModifiers: replace,
			},
			"fingerprint": schema.StringAttribute{
				Computed:      true,
				Description:   "The key's fingerprint.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *sshKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, err := clientsFrom(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	r.clients = c
}

func (r *sshKeyResource) doCreate(ctx context.Context, m *sshKeyModel) error {
	res, err := r.clients.Server.CreateSshKey(ctx, connect.NewRequest(&phronv1.CreateSshKeyRequest{
		Name:      m.Name.ValueString(),
		PublicKey: m.PublicKey.ValueString(),
	}))
	if err != nil {
		return err
	}
	k := res.Msg.GetSshKey()
	m.ID = types.StringValue(k.GetId())
	m.Fingerprint = types.StringValue(k.GetFingerprint())
	return nil
}

func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.doCreate(ctx, &m); err != nil {
		addErr(&resp.Diagnostics, "Creating the SSH key failed", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// doRead finds the key in the list (there is no get call); gone=true when absent.
func (r *sshKeyResource) doRead(ctx context.Context, m *sshKeyModel) (gone bool, err error) {
	res, err := r.clients.Server.ListSshKeys(ctx, connect.NewRequest(&phronv1.ListSshKeysRequest{}))
	if err != nil {
		return false, err
	}
	for _, k := range res.Msg.GetSshKeys() {
		if k.GetId() == m.ID.ValueString() {
			m.Name = types.StringValue(k.GetName())
			m.Fingerprint = types.StringValue(k.GetFingerprint())
			// The API may normalise the key text; keep the configured text
			// unless nothing is known yet (import).
			if m.PublicKey.IsNull() || m.PublicKey.IsUnknown() {
				m.PublicKey = types.StringValue(k.GetPublicKey())
			}
			return false, nil
		}
	}
	return true, nil
}

func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	gone, err := r.doRead(ctx, &m)
	if err != nil {
		addErr(&resp.Diagnostics, "Reading the SSH key failed", err)
		return
	}
	if gone {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Update is never needed: every attribute forces a replacement.
func (r *sshKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.clients.Server.DeleteSshKey(ctx, connect.NewRequest(&phronv1.DeleteSshKeyRequest{SshKeyId: m.ID.ValueString()}))
	if err != nil && !isNotFound(err) {
		addErr(&resp.Diagnostics, "Deleting the SSH key failed", err)
	}
}

func (r *sshKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
