package application

import (
	"reflect"
	"testing"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestWorkspaceJobActionsExposeOnlyPermittedSafeControls(t *testing.T) {
	tests := []struct {
		state domain.JobState
		want  []WorkspaceJobAction
	}{
		{state: domain.JobRunning, want: []WorkspaceJobAction{WorkspaceJobActionCancel, WorkspaceJobActionPause}},
		{state: domain.JobPaused, want: []WorkspaceJobAction{WorkspaceJobActionCancel, WorkspaceJobActionResume}},
		{state: domain.JobFailed, want: []WorkspaceJobAction{WorkspaceJobActionRetry}},
		{state: domain.JobCompleted, want: []WorkspaceJobAction{}},
	}
	for _, test := range tests {
		got := workspaceJobActions(test.state, true, true, true)
		if !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s actions = %#v, want %#v", test.state, got, test.want)
		}
	}
}
