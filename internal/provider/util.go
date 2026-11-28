package provider

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"golang.org/x/oauth2"
)

// Timing knobs. They are variables so the unit tests can shrink them.
var (
	pollInterval     = 5 * time.Second
	retryDelay       = 2 * time.Second
	serverTimeout    = 20 * time.Minute
	storageTimeout   = 15 * time.Minute
	maxRetryAttempts = 4
)

// isAuthFailure reports whether the token endpoint refused the credentials.
// That is not a temporary outage, so it is never retried.
// An answer that is not an OAuth error (a gateway or tunnel page, a 5xx) has
// no error code and is an outage, which the caller may retry.
func isAuthFailure(err error) bool {
	var re *oauth2.RetrieveError
	return errors.As(err, &re) && re.ErrorCode != ""
}

func isNotFound(err error) bool {
	return err != nil && connect.CodeOf(err) == connect.CodeNotFound
}

// describe renders an API error with its Connect code, so a practitioner sees
// "failed_precondition: ..." and not only the message.
func describe(err error) string {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) && isAuthFailure(err) {
		return fmt.Sprintf("the token endpoint refused the API client's credentials (%s). Check client_id, client_secret and project_id (or scope) against the console's API clients page; a deleted client or a rotated secret is refused the same way.", re.ErrorCode)
	}
	var ce *connect.Error
	if errors.As(err, &ce) {
		return fmt.Sprintf("%s: %s", ce.Code(), ce.Message())
	}
	return err.Error()
}

func addErr(d *diag.Diagnostics, summary string, err error) {
	d.AddError(summary, describe(err))
}

// newRequestID returns a random UUID (version 4). A create call makes one and
// reuses it across its own retries, so a retry cannot create a second object.
func newRequestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// retryUnavailable runs f again while it fails with Unavailable (the code a
// Connect client infers from a gateway 429 or a dropped connection). Only
// calls that are idempotent may use it.
func retryUnavailable(ctx context.Context, f func() error) error {
	var err error
	for attempt := 0; attempt < maxRetryAttempts; attempt++ {
		err = f()
		if err == nil || isAuthFailure(err) || connect.CodeOf(err) != connect.CodeUnavailable {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(retryDelay * time.Duration(attempt+1)):
		}
	}
	return err
}

// pollUntil calls f every pollInterval until it reports done, fails, or the
// timeout passes. An Unavailable error from f is treated as "not yet".
func pollUntil(ctx context.Context, what string, timeout time.Duration, f func(ctx context.Context) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		done, err := f(ctx)
		if err != nil && (isAuthFailure(err) || connect.CodeOf(err) != connect.CodeUnavailable) {
			return err
		}
		if err == nil && done {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for %s", what)
		case <-time.After(pollInterval):
		}
	}
}

func setToStrings(ctx context.Context, s types.Set) ([]string, error) {
	if s.IsNull() || s.IsUnknown() {
		return nil, nil
	}
	var out []string
	if d := s.ElementsAs(ctx, &out, false); d.HasError() {
		return nil, fmt.Errorf("reading set: %s", d.Errors()[0].Detail())
	}
	return out, nil
}

func stringsToSet(in []string) types.Set {
	elems := make([]attr.Value, 0, len(in))
	for _, s := range in {
		elems = append(elems, types.StringValue(s))
	}
	return types.SetValueMust(types.StringType, elems)
}

// enumWord turns SERVER_STATE_RUNNING into "running".
func enumWord(prefix, name string) string {
	return strings.ToLower(strings.TrimPrefix(name, prefix))
}

// replaceUnlessImported replaces the resource when the attribute changes, except when the state holds no value for it:
// the API never returns some arguments (user_data), so an imported resource has none, and giving it its configured
// value must not replace it.
func replaceUnlessImported() planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(
		func(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
			resp.RequiresReplace = !req.StateValue.IsNull()
		},
		"Changing it replaces the resource, unless the state has no value (an imported resource).",
		"Changing it replaces the resource, unless the state has no value (an imported resource).",
	)
}
