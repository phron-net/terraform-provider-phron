// Package provider implements the Phron Terraform provider.
package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/phron-net/api/gen/zonhoren/phron/v1/phronv1connect"
)

const (
	defaultEndpoint = "https://api.phron.net"
	defaultTokenURL = "https://auth.atestum.com/oauth/v2/token"
)

// Clients holds the typed API clients every resource and data source uses.
type Clients struct {
	Server  phronv1connect.ServerServiceClient
	Storage phronv1connect.StorageServiceClient
}

// NewClients builds the clients over httpClient. The API is plain Connect
// (JSON over HTTP POST), as the console's API clients page shows with curl.
func NewClients(httpClient connect.HTTPClient, endpoint string) *Clients {
	endpoint = strings.TrimRight(endpoint, "/")
	return &Clients{
		Server:  phronv1connect.NewServerServiceClient(httpClient, endpoint, connect.WithProtoJSON()),
		Storage: phronv1connect.NewStorageServiceClient(httpClient, endpoint, connect.WithProtoJSON()),
	}
}

var _ provider.Provider = (*phronProvider)(nil)

type phronProvider struct {
	version string
}

// New returns the provider constructor for providerserver.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &phronProvider{version: version} }
}

type providerModel struct {
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	ProjectID    types.String `tfsdk:"project_id"`
	Scope        types.String `tfsdk:"scope"`
	Endpoint     types.String `tfsdk:"endpoint"`
	TokenURL     types.String `tfsdk:"token_url"`
}

func (p *phronProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "phron"
	resp.Version = p.version
}

func (p *phronProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Phron Servers and Object Storage through the Phron API, signed in as an API client (an OAuth client-credentials machine user of your Organization).",
		Attributes: map[string]schema.Attribute{
			"client_id": schema.StringAttribute{
				Optional:    true,
				Description: "The API client's client ID (shown once on the console's API clients page). Defaults to the PHRON_CLIENT_ID environment variable.",
			},
			"client_secret": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "The API client's client secret. Defaults to the PHRON_CLIENT_SECRET environment variable.",
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Description: "The Phron project id shown in the scope on the console's API clients page. When set, the OAuth scope is built from it. Defaults to the PHRON_PROJECT_ID environment variable. Ignored when scope is set.",
			},
			"scope": schema.StringAttribute{
				Optional:    true,
				Description: "The complete space-separated OAuth scope, as shown on the console's API clients page. Overrides project_id. Defaults to the PHRON_SCOPE environment variable.",
			},
			"endpoint": schema.StringAttribute{
				Optional:    true,
				Description: "The API base URL. Defaults to the PHRON_ENDPOINT environment variable, then " + defaultEndpoint + ".",
			},
			"token_url": schema.StringAttribute{
				Optional:    true,
				Description: "The OAuth token endpoint. Defaults to the PHRON_TOKEN_URL environment variable, then " + defaultTokenURL + ".",
			},
		},
	}
}

// pick returns the configured value, else the environment variable, else the
// fallback. An unknown configured value is reported through ok=false.
func pick(v types.String, env, fallback string) (value string, ok bool) {
	if v.IsUnknown() {
		return "", false
	}
	if !v.IsNull() && v.ValueString() != "" {
		return v.ValueString(), true
	}
	if e := os.Getenv(env); e != "" {
		return e, true
	}
	return fallback, true
}

// ScopeFor builds the OAuth scope the API requires for a Phron project: the
// OpenID scope, the project audience and the resource-owner claim.
func ScopeFor(projectID string) string {
	return "openid urn:zitadel:iam:org:project:id:" + projectID + ":aud urn:zitadel:iam:user:resourceowner"
}

func (p *phronProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	clientID, ok1 := pick(cfg.ClientID, "PHRON_CLIENT_ID", "")
	clientSecret, ok2 := pick(cfg.ClientSecret, "PHRON_CLIENT_SECRET", "")
	projectID, ok3 := pick(cfg.ProjectID, "PHRON_PROJECT_ID", "")
	scope, ok4 := pick(cfg.Scope, "PHRON_SCOPE", "")
	endpoint, ok5 := pick(cfg.Endpoint, "PHRON_ENDPOINT", defaultEndpoint)
	tokenURL, ok6 := pick(cfg.TokenURL, "PHRON_TOKEN_URL", defaultTokenURL)
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) {
		resp.Diagnostics.AddError("Unknown provider configuration",
			"A provider attribute depends on a value that is not known until apply. Set it from a variable or an environment variable.")
		return
	}
	if clientID == "" {
		resp.Diagnostics.AddError("Missing client_id", "Set client_id or the PHRON_CLIENT_ID environment variable.")
	}
	if clientSecret == "" {
		resp.Diagnostics.AddError("Missing client_secret", "Set client_secret or the PHRON_CLIENT_SECRET environment variable.")
	}
	if scope == "" && projectID == "" {
		resp.Diagnostics.AddError("Missing scope",
			"Set project_id (or PHRON_PROJECT_ID), or the complete scope (or PHRON_SCOPE), both as shown on the console's API clients page.")
	}
	if resp.Diagnostics.HasError() {
		return
	}
	if scope == "" {
		scope = ScopeFor(projectID)
	}

	// The token source must outlive the request context, so it gets its own.
	base := &http.Client{Timeout: 60 * time.Second}
	tokenCtx := context.WithValue(context.Background(), oauth2.HTTPClient, base)
	cc := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     tokenURL,
		Scopes:       strings.Fields(scope),
	}
	httpClient := cc.Client(tokenCtx)
	httpClient.Timeout = 120 * time.Second

	clients := NewClients(httpClient, endpoint)
	resp.DataSourceData = clients
	resp.ResourceData = clients
}

func (p *phronProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewServerResource,
		NewSSHKeyResource,
		NewStorageBucketResource,
		NewStorageBucketLimitResource,
		NewStorageKeyResource,
	}
}

func (p *phronProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewLocationsDataSource,
		NewOperatingSystemsDataSource,
		NewServerQuoteDataSource,
		NewStorageOverviewDataSource,
	}
}

// clientsFrom extracts the provider's clients in a Configure method.
func clientsFrom(data any) (*Clients, error) {
	if data == nil {
		return nil, nil
	}
	c, ok := data.(*Clients)
	if !ok {
		return nil, fmt.Errorf("unexpected provider data type %T", data)
	}
	return c, nil
}
