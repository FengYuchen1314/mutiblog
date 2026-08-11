package taskstore

import "testing"

func TestValidateRecognizesSharedTaskKinds(t *testing.T) {
	tests := []Header{
		{SchemaVersion: 1, ID: "backup-task-example", Kind: "Backup"},
		{SchemaVersion: 1, ID: "translation-example", Kind: "Translation"},
		{SchemaVersion: 1, ID: "20260811T010203.000000000Z-aabbccdd", Kind: "StaticBuild"},
	}
	for _, header := range tests {
		if err := Validate(header, header.ID); err != nil {
			t.Fatalf("Validate(%#v) error = %v", header, err)
		}
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
		{Header{SchemaVersion: 1, ID: "other-example", Kind: "Other"}, "other-example"},
		{Header{SchemaVersion: 2, ID: "translation-example", Kind: "Translation"}, "translation-example"},
	}
	for _, test := range tests {
		if err := Validate(test.header, test.expectedID); err == nil {
			t.Fatalf("Validate(%#v, %q) unexpectedly succeeded", test.header, test.expectedID)
		}
	}
}
