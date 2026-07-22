package jobs

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/domain"
)

func TestJobTransitions(t *testing.T) {
	for _, transition := range [][2]domain.JobState{
		{domain.JobQueued, domain.JobRunning},
		{domain.JobRunning, domain.JobCompleted},
		{domain.JobRunning, domain.JobPartial},
		{domain.JobPaused, domain.JobQueued},
		{domain.JobBlockedAuth, domain.JobQueued},
	} {
		if err := ValidateTransition(transition[0], transition[1]); err != nil {
			t.Fatalf("ValidateTransition(%s, %s) = %v", transition[0], transition[1], err)
		}
	}
	if err := ValidateTransition(domain.JobCompleted, domain.JobRunning); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("terminal transition error = %v", err)
	}
}

func TestBackoffIsBoundedAndDeterministic(t *testing.T) {
	backoff := Backoff{Base: time.Second, Max: 5 * time.Second, Jitter: func(delay time.Duration) time.Duration { return delay / 10 }}
	if got := backoff.Delay(1); got != 1100*time.Millisecond {
		t.Fatalf("Delay(1) = %s", got)
	}
	if got := backoff.Delay(3); got != 4400*time.Millisecond {
		t.Fatalf("Delay(3) = %s", got)
	}
	if got := backoff.Delay(10); got != 5*time.Second {
		t.Fatalf("Delay(10) = %s", got)
	}
}

func TestClassifiedFailureControlsRetry(t *testing.T) {
	err := &ClassifiedError{Class: FailureThrottling, Retryable: true, Err: errors.New("rate limited")}
	class, retryable := Classify(err)
	if class != FailureThrottling || !retryable || err.Error() != "rate limited" {
		t.Fatalf("Classify() = %s, %v, %v", class, retryable, err)
	}
}

func TestEncodeCheckpointRedactsStructuredAndRawSecrets(t *testing.T) {
	checkpoint := map[string]any{
		"url": "https://mp.weixin.qq.com/s/example?pass_ticket=pass-secret&offset=20",
		"raw": json.RawMessage(`{"appmsg_token":"raw-secret","cursor":"visible"}`),
	}
	encoded, err := encodeCheckpoint(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"pass-secret", "raw-secret"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("checkpoint leaked %q: %s", forbidden, encoded)
		}
	}
	for _, retained := range []string{"offset", "20", "cursor", "visible"} {
		if !strings.Contains(string(encoded), retained) {
			t.Fatalf("checkpoint removed %q: %s", retained, encoded)
		}
	}
}
