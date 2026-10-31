package domain

import "testing"

func TestPullRequestReviewStateRetainsIndependentSources(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reviews  []RemoteReview
		external []ExternalReview
		want     string
	}{
		{"none", nil, nil, "none"},
		{"pending external", nil, []ExternalReview{{State: "pending"}}, "none"},
		{"current approval", []RemoteReview{{State: "APPROVED", Commit: "head"}}, []ExternalReview{{State: "pending"}}, "approved"},
		{"old approval", []RemoteReview{{State: "APPROVED", Commit: "old"}}, nil, "none"},
		{"old human request", []RemoteReview{{State: "CHANGES_REQUESTED", Commit: "old"}}, []ExternalReview{{State: "success"}}, "changes_requested"},
		{"independent human request", []RemoteReview{{State: "APPROVED", Commit: "head"}, {State: "CHANGES_REQUESTED", Commit: "head"}}, nil, "changes_requested"},
		{"external failure", []RemoteReview{{State: "APPROVED", Commit: "head"}}, []ExternalReview{{State: "failure"}}, "changes_requested"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PullRequestReviewState(tc.reviews, tc.external, "head"); got != tc.want {
				t.Fatal(got, tc.want)
			}
		})
	}
}
