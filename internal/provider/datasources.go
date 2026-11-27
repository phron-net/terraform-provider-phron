package provider

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	phronv1 "github.com/phron-net/api/gen/zonhoren/phron/v1"
)

func configureData(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse, dst **Clients) {
	c, err := clientsFrom(req.ProviderData)
	if err != nil {
		resp.Diagnostics.AddError("Unexpected provider data", err.Error())
		return
	}
	*dst = c
}

// ---- phron_locations ----

var _ datasource.DataSourceWithConfigure = (*locationsDataSource)(nil)

// NewLocationsDataSource returns the phron_locations data source.
func NewLocationsDataSource() datasource.DataSource { return &locationsDataSource{} }

type locationsDataSource struct{ clients *Clients }

type locationModel struct {
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	Country           types.String `tfsdk:"country"`
	Available         types.Bool   `tfsdk:"available"`
	UnavailableReason types.String `tfsdk:"unavailable_reason"`
}

type locationsModel struct {
	ID        types.String    `tfsdk:"id"`
	Locations []locationModel `tfsdk:"locations"`
}

func locationAttrs() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id":                 schema.StringAttribute{Computed: true, Description: "The Location id, as resources take it in `location`."},
		"name":               schema.StringAttribute{Computed: true, Description: "The city."},
		"country":            schema.StringAttribute{Computed: true, Description: "The country."},
		"available":          schema.BoolAttribute{Computed: true, Description: "Whether something new can be created there now."},
		"unavailable_reason": schema.StringAttribute{Computed: true, Description: "Why not, when available is false."},
	}
}

func locationsFrom(in []*phronv1.Location) []locationModel {
	out := make([]locationModel, 0, len(in))
	for _, l := range in {
		out = append(out, locationModel{
			ID:                types.StringValue(l.GetId()),
			Name:              types.StringValue(l.GetName()),
			Country:           types.StringValue(l.GetCountry()),
			Available:         types.BoolValue(l.GetAvailable()),
			UnavailableReason: types.StringValue(l.GetUnavailableReason()),
		})
	}
	return out
}

func (d *locationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_locations"
}

func (d *locationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The Locations a Server can live in, each with whether new Servers can be created there now.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Always \"locations\"."},
			"locations": schema.ListNestedAttribute{
				Computed:     true,
				Description:  "Every Location, offered or not.",
				NestedObject: schema.NestedAttributeObject{Attributes: locationAttrs()},
			},
		},
	}
}

func (d *locationsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureData(req, resp, &d.clients)
}

func (d *locationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	res, err := d.clients.Server.ListServerLocations(ctx, connect.NewRequest(&phronv1.ListServerLocationsRequest{}))
	if err != nil {
		addErr(&resp.Diagnostics, "Listing Locations failed", err)
		return
	}
	m := locationsModel{ID: types.StringValue("locations"), Locations: locationsFrom(res.Msg.GetLocations())}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---- phron_operating_systems ----

var _ datasource.DataSourceWithConfigure = (*operatingSystemsDataSource)(nil)

// NewOperatingSystemsDataSource returns the phron_operating_systems data source.
func NewOperatingSystemsDataSource() datasource.DataSource { return &operatingSystemsDataSource{} }

type operatingSystemsDataSource struct{ clients *Clients }

type operatingSystemModel struct {
	ID          types.String `tfsdk:"id"`
	DisplayName types.String `tfsdk:"display_name"`
	MinDiskGiB  types.Int64  `tfsdk:"min_disk_gib"`
	LoginUser   types.String `tfsdk:"login_user"`
}

type operatingSystemsModel struct {
	ID               types.String           `tfsdk:"id"`
	OperatingSystems []operatingSystemModel `tfsdk:"operating_systems"`
}

func (d *operatingSystemsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_operating_systems"
}

func (d *operatingSystemsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The Operating Systems a new Server can be created from.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "Always \"operating_systems\"."},
			"operating_systems": schema.ListNestedAttribute{
				Computed:    true,
				Description: "The available Operating Systems.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id":           schema.StringAttribute{Computed: true, Description: "The id, as phron_server takes it in `operating_system_id`."},
					"display_name": schema.StringAttribute{Computed: true, Description: "The name for display."},
					"min_disk_gib": schema.Int64Attribute{Computed: true, Description: "The smallest disk, in GiB, a Server of this Operating System needs."},
					"login_user":   schema.StringAttribute{Computed: true, Description: "The user name to ssh in as."},
				}},
			},
		},
	}
}

func (d *operatingSystemsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureData(req, resp, &d.clients)
}

func (d *operatingSystemsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	res, err := d.clients.Server.ListOperatingSystems(ctx, connect.NewRequest(&phronv1.ListOperatingSystemsRequest{}))
	if err != nil {
		addErr(&resp.Diagnostics, "Listing Operating Systems failed", err)
		return
	}
	m := operatingSystemsModel{ID: types.StringValue("operating_systems"), OperatingSystems: []operatingSystemModel{}}
	for _, os := range res.Msg.GetOperatingSystems() {
		m.OperatingSystems = append(m.OperatingSystems, operatingSystemModel{
			ID:          types.StringValue(os.GetId()),
			DisplayName: types.StringValue(os.GetDisplayName()),
			MinDiskGiB:  types.Int64Value(int64(os.GetMinDiskGib())),
			LoginUser:   types.StringValue(os.GetLoginUser()),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---- phron_server_quote ----

var _ datasource.DataSourceWithConfigure = (*serverQuoteDataSource)(nil)

// NewServerQuoteDataSource returns the phron_server_quote data source.
func NewServerQuoteDataSource() datasource.DataSource { return &serverQuoteDataSource{} }

type serverQuoteDataSource struct{ clients *Clients }

type serverQuoteModel struct {
	ID                types.String `tfsdk:"id"`
	Tier              types.String `tfsdk:"tier"`
	VCPUs             types.Int64  `tfsdk:"vcpus"`
	MemoryGiB         types.Int64  `tfsdk:"memory_gib"`
	DiskGiB           types.Int64  `tfsdk:"disk_gib"`
	Location          types.String `tfsdk:"location"`
	OperatingSystemID types.String `tfsdk:"operating_system_id"`
	HourlyUSD         types.String `tfsdk:"hourly_usd"`
	BackupHourlyUSD   types.String `tfsdk:"backup_hourly_usd"`
	OwnPrefix         types.Bool   `tfsdk:"own_prefix"`
	Available         types.Bool   `tfsdk:"available"`
	UnavailableReason types.String `tfsdk:"unavailable_reason"`
	Problems          types.List   `tfsdk:"problems"`
}

func (d *serverQuoteDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server_quote"
}

func (d *serverQuoteDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Checks a Server size against the size rules in effect and prices it. A size that breaks a rule is an answer (problems), not an error.",
		Attributes: map[string]schema.Attribute{
			"id":                  schema.StringAttribute{Computed: true, Description: "A description of the quoted size."},
			"tier":                schema.StringAttribute{Optional: true, Description: "\"shared\" (default) or \"dedicated\"."},
			"vcpus":               schema.Int64Attribute{Required: true, Description: "Number of vCPUs."},
			"memory_gib":          schema.Int64Attribute{Required: true, Description: "Memory in GiB."},
			"disk_gib":            schema.Int64Attribute{Required: true, Description: "Disk in GiB."},
			"location":            schema.StringAttribute{Optional: true, Description: "The Location id; when empty, available is false."},
			"operating_system_id": schema.StringAttribute{Optional: true, Description: "Also check the size against this Operating System's minimum disk."},
			"hourly_usd":          schema.StringAttribute{Computed: true, Description: "The hourly price in USD as a decimal string; empty when problems is not empty."},
			"backup_hourly_usd":   schema.StringAttribute{Computed: true, Description: "The hourly price of daily backups in USD."},
			"own_prefix":          schema.BoolAttribute{Computed: true, Description: "Whether a Server of this size owns its own /64."},
			"available":           schema.BoolAttribute{Computed: true, Description: "Whether a Server of this size can be created now in the Location."},
			"unavailable_reason":  schema.StringAttribute{Computed: true, Description: "Why not, when available is false."},
			"problems":            schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Size rules the size breaks, as \"field: reason\"."},
		},
	}
}

func (d *serverQuoteDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureData(req, resp, &d.clients)
}

func (d *serverQuoteDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m serverQuoteModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tier, err := tierToProto(m.Tier.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid tier", err.Error())
		return
	}
	q := &phronv1.QuoteServerRequest{
		Size: &phronv1.ServerSize{
			Tier:      tier,
			Vcpus:     int32(m.VCPUs.ValueInt64()),
			MemoryGib: int32(m.MemoryGiB.ValueInt64()),
			DiskGib:   int32(m.DiskGiB.ValueInt64()),
		},
		Location: m.Location.ValueString(),
	}
	if !m.OperatingSystemID.IsNull() {
		os := m.OperatingSystemID.ValueString()
		q.OperatingSystemId = &os
	}
	res, err := d.clients.Server.QuoteServer(ctx, connect.NewRequest(q))
	if err != nil {
		addErr(&resp.Diagnostics, "Quoting the Server failed", err)
		return
	}
	out := res.Msg
	m.ID = types.StringValue(fmt.Sprintf("%s/%d/%d/%d/%s", tierFromProto(tier), q.Size.Vcpus, q.Size.MemoryGib, q.Size.DiskGib, q.Location))
	m.HourlyUSD = types.StringValue(out.GetPrice().GetHourlyUsd())
	m.BackupHourlyUSD = types.StringValue(out.GetPrice().GetBackupHourlyUsd())
	m.OwnPrefix = types.BoolValue(out.GetOwnPrefix())
	m.Available = types.BoolValue(out.GetAvailable())
	m.UnavailableReason = types.StringValue(out.GetUnavailableReason())
	problems := make([]attr.Value, 0, len(out.GetProblems()))
	for _, p := range out.GetProblems() {
		problems = append(problems, types.StringValue(p.GetField()+": "+p.GetReason()))
	}
	m.Problems = types.ListValueMust(types.StringType, problems)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// ---- phron_storage_overview ----

var _ datasource.DataSourceWithConfigure = (*storageOverviewDataSource)(nil)

// NewStorageOverviewDataSource returns the phron_storage_overview data source.
func NewStorageOverviewDataSource() datasource.DataSource { return &storageOverviewDataSource{} }

type storageOverviewDataSource struct{ clients *Clients }

type storageOverviewModel struct {
	ID                 types.String    `tfsdk:"id"`
	WorkspaceID        types.String    `tfsdk:"workspace_id"`
	Location           types.String    `tfsdk:"location"`
	State              types.String    `tfsdk:"state"`
	EndpointURL        types.String    `tfsdk:"endpoint_url"`
	Region             types.String    `tfsdk:"region"`
	BucketNamePrefix   types.String    `tfsdk:"bucket_name_prefix"`
	StoredBytes        types.Int64     `tfsdk:"stored_bytes"`
	ObjectCount        types.Int64     `tfsdk:"object_count"`
	BucketCount        types.Int64     `tfsdk:"bucket_count"`
	KeyCount           types.Int64     `tfsdk:"key_count"`
	MonthToDateCost    types.String    `tfsdk:"month_to_date_cost"`
	Currency           types.String    `tfsdk:"currency"`
	PricePerGBMonthUSD types.String    `tfsdk:"price_per_gb_month_usd"`
	Locations          []locationModel `tfsdk:"locations"`
}

func (d *storageOverviewDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_storage_overview"
}

func (d *storageOverviewDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The state, endpoint, usage and price of a Workspace's Object Storage. Safe to read before any storage exists (state \"not_created\").",
		Attributes: map[string]schema.Attribute{
			"id":                     schema.StringAttribute{Computed: true, Description: "The Workspace id the overview describes."},
			"workspace_id":           schema.StringAttribute{Optional: true, Description: "The Workspace; empty means the Organization's default Workspace."},
			"location":               schema.StringAttribute{Optional: true, Description: "The Location of the storage, when the Workspace has storage in more than one."},
			"state":                  schema.StringAttribute{Computed: true, Description: "The storage state, for example \"active\" or \"not_created\"."},
			"endpoint_url":           schema.StringAttribute{Computed: true, Description: "The S3 endpoint; empty until the storage exists."},
			"region":                 schema.StringAttribute{Computed: true, Description: "The S3 signing region."},
			"bucket_name_prefix":     schema.StringAttribute{Computed: true, Description: "The text every bucket name of a shared Cell starts with."},
			"stored_bytes":           schema.Int64Attribute{Computed: true, Description: "Logical stored bytes (what is billed)."},
			"object_count":           schema.Int64Attribute{Computed: true, Description: "Number of objects."},
			"bucket_count":           schema.Int64Attribute{Computed: true, Description: "Number of buckets."},
			"key_count":              schema.Int64Attribute{Computed: true, Description: "Number of Storage Access Keys."},
			"month_to_date_cost":     schema.StringAttribute{Computed: true, Description: "Storage charges so far this calendar month, in the currency below."},
			"currency":               schema.StringAttribute{Computed: true, Description: "The ISO 4217 currency of month_to_date_cost."},
			"price_per_gb_month_usd": schema.StringAttribute{Computed: true, Description: "The price per GB-month in USD."},
			"locations": schema.ListNestedAttribute{
				Computed:     true,
				Description:  "Every Location, with whether new storage can be created there now.",
				NestedObject: schema.NestedAttributeObject{Attributes: locationAttrs()},
			},
		},
	}
}

func (d *storageOverviewDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureData(req, resp, &d.clients)
}

func (d *storageOverviewDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m storageOverviewModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	res, err := d.clients.Storage.GetStorageOverview(ctx, connect.NewRequest(&phronv1.GetStorageOverviewRequest{
		WorkspaceId: m.WorkspaceID.ValueString(),
		Location:    m.Location.ValueString(),
	}))
	if err != nil {
		addErr(&resp.Diagnostics, "Reading the storage overview failed", err)
		return
	}
	o := res.Msg
	m.ID = types.StringValue(o.GetWorkspaceId())
	m.State = types.StringValue(enumWord("STORAGE_TENANT_STATE_", o.GetState().String()))
	m.EndpointURL = types.StringValue(o.GetEndpointUrl())
	m.Region = types.StringValue(o.GetRegion())
	m.BucketNamePrefix = types.StringValue(o.GetBucketNamePrefix())
	m.StoredBytes = types.Int64Value(o.GetUsage().GetStoredBytes())
	m.ObjectCount = types.Int64Value(o.GetUsage().GetObjectCount())
	m.BucketCount = types.Int64Value(int64(o.GetUsage().GetBucketCount()))
	m.KeyCount = types.Int64Value(int64(o.GetUsage().GetKeyCount()))
	m.MonthToDateCost = types.StringValue(o.GetUsage().GetMonthToDateCostUsd())
	m.Currency = types.StringValue(o.GetUsage().GetCurrency())
	m.PricePerGBMonthUSD = types.StringValue(o.GetPricing().GetPricePerGbMonthUsd())
	m.Locations = locationsFrom(o.GetLocations())
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
