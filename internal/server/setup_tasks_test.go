package server

import (
	"context"
	"testing"

	"github.com/FengYuchen1314/mutiblog/internal/domain"
	"github.com/FengYuchen1314/mutiblog/internal/platform/fsrepo"
	"github.com/FengYuchen1314/mutiblog/internal/publisher"
	"github.com/FengYuchen1314/mutiblog/internal/taskstore"
)

const setupBuildTaskID = "20260811T010203.000000000Z-aabbccdd"

func TestInitialSetupBuildRequestRejectsExistingTaskBeforeInitialization(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteYAML("state/tasks/"+setupBuildTaskID+".yaml", publisher.Task{
		SchemaVersion: domain.SchemaVersion, ID: setupBuildTaskID, Kind: "StaticBuild", Status: "succeeded",
	}, false); err != nil {
		t.Fatal(err)
	}
	app := &Server{repository: repository}
	ctx := publisher.WithBuildRequest(context.Background(), publisher.BuildRequest{TaskID: setupBuildTaskID})
	if _, err := app.initialSetupBuildRequest(ctx); err == nil {
		t.Fatal("initial setup accepted an existing task ID")
	}
	if initialized, err := repository.Exists("config/initialized"); err != nil || initialized {
		t.Fatalf("collision check changed initialization state: initialized=%v err=%v", initialized, err)
	}
}

func TestInitialSetupBuildRequestGeneratesTrackedInitialBuild(t *testing.T) {
	repository, err := fsrepo.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request, err := (&Server{repository: repository}).initialSetupBuildRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !taskstore.ValidStaticBuildID(request.TaskID) || request.Operation != "initial-setup" || request.SubjectKind != "" || request.SubjectID != "" {
		t.Fatalf("initial build request = %#v", request)
	}
}
