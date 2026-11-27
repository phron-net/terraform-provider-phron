package provider

import (
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"phron": providerserver.NewProtocol6WithError(New("test")()),
	}
}

func testAccPreCheck(t *testing.T) {
	t.Helper()
	for _, v := range []string{"PHRON_CLIENT_ID", "PHRON_CLIENT_SECRET", "PHRON_PROJECT_ID"} {
		if os.Getenv(v) == "" {
			t.Fatalf("%s must be set for acceptance tests", v)
		}
	}
}

func TestScopeFor(t *testing.T) {
	got := ScopeFor("123")
	want := "openid urn:zitadel:iam:org:project:id:123:aud urn:zitadel:iam:user:resourceowner"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// Acceptance tests create real, billed objects. They run only with TF_ACC=1
// and credentials in the environment, never in CI.
func TestAccStorageBucket(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "phron" {}

resource "phron_storage_bucket" "test" {
  name = "tf-acc-test"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("phron_storage_bucket.test", "full_name"),
				),
			},
		},
	})
}

func TestAccLocationsDataSource(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "phron" {}

data "phron_locations" "all" {}
`,
				Check: resource.TestCheckResourceAttrSet("data.phron_locations.all", "id"),
			},
		},
	})
}
