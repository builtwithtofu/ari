package daemon

import "testing"

func TestPresentationStatusLabelsCompletedDistinctly(t *testing.T) {
	status, label := normalizePresentationStatus("completed")
	if status != PresentationStatusStopped || label != "Completed" {
		t.Fatalf("completed presentation = %s/%q, want stopped bucket with Completed label", status, label)
	}

	status, label = normalizePresentationStatus("stopped")
	if status != PresentationStatusStopped || label != "Stopped" {
		t.Fatalf("stopped presentation = %s/%q, want stopped bucket with Stopped label", status, label)
	}
}

func TestPresentationMapsAttentionStates(t *testing.T) {
	for _, tc := range []struct {
		raw       string
		want      PresentationStatus
		wantLabel string
	}{
		{raw: "none", want: PresentationStatusReady, wantLabel: "Ready"},
		{raw: "auth", want: PresentationStatusNeedsAuth, wantLabel: "Needs auth"},
	} {
		status, label := normalizePresentationStatus(tc.raw)
		if status != tc.want || label != tc.wantLabel {
			t.Fatalf("%s presentation = %s/%q, want %s/%q", tc.raw, status, label, tc.want, tc.wantLabel)
		}
	}
}

func TestCompletedStickySessionPresentsReady(t *testing.T) {
	p := sessionPresentation("sticky-1", "", "codex", "sticky", "completed")
	if p.Status != PresentationStatusReady || p.StatusLabel != "Ready" || p.Source == nil || p.Source.NativeStatus != "completed" {
		t.Fatalf("sticky completed presentation = %#v, want ready with completed source status", p)
	}
}
