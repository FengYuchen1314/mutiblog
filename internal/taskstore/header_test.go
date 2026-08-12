package taskstore

import "testing"

func TestValidateRecognizesSharedTaskKinds(t *testing.T) {
	tests := []Header{
		{SchemaVersion: 1, ID: "backup-task-example", Kind: "Backup"},
		{SchemaVersion: 1, ID: "translation-example", Kind: "Translation"},
		{SchemaVersion: 1, ID: "20260811T010203.000000000Z-aabbccdd", Kind: "StaticBuild"},
		{SchemaVersion: 1, ID: "scheduled-publish-20260811T010203.000000000Z-aabbccdd", Kind: "ScheduledPublish"},
		{SchemaVersion: 1, ID: "index-rebuild-20260811T010203.000000000Z-aabbccdd", Kind: "IndexRebuild"},
		{SchemaVersion: 1, ID: "locale-provision-20260811T010203.000000000Z-aabbccdd", Kind: "LocaleProvision"},
	}
	for _, header := range tests {
		if err := Validate(header, header.ID); err != nil {
			t.Fatalf("Validate(%#v) error = %v", header, err)
		}
	}
}

func TestProgressNeverMovesBackwardsAndOnlySuccessCompletes(t *testing.T) {
	progress := Advance(Progress{}, "render", 2, 4, 70, "rendering")
	progress = Advance(progress, "validate", 1, 10, 20, "validating")
	if progress.Percent != 70 || progress.Current != 2 || progress.Total != 10 {
		t.Fatalf("regressed progress = %#v", progress)
	}
	progress = Advance(progress, "activate", 3, 2, 80, "activating")
	if progress.Current != 3 || progress.Total != 10 {
		t.Fatalf("regressed progress units = %#v", progress)
	}
	failed := Advance(progress, progress.Phase, progress.Current, progress.Total, progress.Percent, "failed")
	if failed.Percent != 80 {
		t.Fatalf("failed progress changed = %#v", failed)
	}
	completed := Complete(progress, "done")
	if completed.Percent != 100 || completed.Current != completed.Total {
		t.Fatalf("completed progress = %#v", completed)
	}
}

func TestValidateRejectsDamagedOrUnknownRecords(t *testing.T) {
	tests := []struct {
		header     Header
		expectedID string
	}{
		{Header{SchemaVersion: 1, ID: "translation-other", Kind: "Translation"}, "translation-expected"},
		{Header{SchemaVersion: 1, ID: "backup-example", Kind: "Backup"}, "backup-example"},
		{Header{SchemaVersion: 1, ID: "static-build-example", Kind: "StaticBuild"}, "static-build-example"},
		{Header{SchemaVersion: 1, ID: "index-rebuild-example", Kind: "IndexRebuild"}, "index-rebuild-example"},
		{Header{SchemaVersion: 1, ID: "other-example", Kind: "Other"}, "other-example"},
		{Header{SchemaVersion: 2, ID: "translation-example", Kind: "Translation"}, "translation-example"},
	}
	for _, test := range tests {
		if err := Validate(test.header, test.expectedID); err == nil {
			t.Fatalf("Validate(%#v, %q) unexpectedly succeeded", test.header, test.expectedID)
		}
	}
}
