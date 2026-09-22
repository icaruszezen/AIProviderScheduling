package channelmonitor

import "testing"

func TestClassifyErrorTaxonomy(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{status: 401, body: "nope", want: CategoryAuthentication},
		{status: 429, body: "slow down", want: CategoryRateOrCapacity},
		{status: 500, body: "boom", want: CategoryUpstream5xx},
		{status: 403, body: "forbidden", want: CategoryUpstreamForbidden},
		{status: 404, body: "missing", want: CategoryNotFound},
		{status: 200, body: "content policy violation", want: CategoryContentPolicy},
		{status: 200, body: "quota exceeded", want: CategoryQuotaOrBalance},
		{status: 408, body: "", want: CategoryTimeout},
		{status: 499, body: "", want: CategoryClientCancelled},
		{status: 400, body: "something else", want: CategoryOther},
	}
	for _, tc := range cases {
		got := Classify(ErrorInput{StatusCode: tc.status, Message: tc.body})
		if got != tc.want {
			t.Fatalf("Classify(%d, %q) = %s, want %s", tc.status, tc.body, got, tc.want)
		}
	}
}
