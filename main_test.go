package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codex-429-autoban-sizhe233/cpasdk/pluginabi"
	"codex-429-autoban-sizhe233/cpasdk/pluginapi"
)

func TestHandleSchedulerPickAllBannedRejectsWithoutFallback(t *testing.T) {
	restoreBans := replaceBansForTest(map[string]banEntry{
		"auth-a": {ResetAt: time.Now().Add(30 * time.Minute), Window: "5h"},
		"auth-b": {ResetAt: time.Now().Add(15 * time.Minute), Window: "5h"},
	})
	t.Cleanup(restoreBans)

	raw := schedulerPickRequest(t,
		pluginapi.SchedulerAuthCandidate{ID: "auth-a", Provider: providerCodex},
		pluginapi.SchedulerAuthCandidate{ID: "auth-b", Provider: providerCodex},
	)
	response, err := handleSchedulerPick(raw)
	if err != nil {
		t.Fatalf("handleSchedulerPick() error = %v", err)
	}

	var envelope pluginabi.Envelope
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.OK || envelope.Error != nil {
		t.Fatalf("response = %#v, want successful scheduler decision", envelope)
	}
	var decision pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(envelope.Result, &decision); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	if !decision.Handled || !decision.Reject || decision.AuthID != "" || decision.DelegateBuiltin != "" {
		t.Fatalf("decision = %#v, want terminal rejection without a selection", decision)
	}
	if decision.RejectCode != "auth_unavailable" || !strings.Contains(decision.RejectReason, "earliest reset at") {
		t.Fatalf("decision = %#v, want auth_unavailable with earliest reset time", decision)
	}
}

func TestHandleSchedulerPickSelectsUnbannedCredential(t *testing.T) {
	restoreBans := replaceBansForTest(map[string]banEntry{
		"auth-banned": {ResetAt: time.Now().Add(30 * time.Minute), Window: "5h"},
	})
	t.Cleanup(restoreBans)

	raw := schedulerPickRequest(t,
		pluginapi.SchedulerAuthCandidate{ID: "auth-banned", Provider: providerCodex},
		pluginapi.SchedulerAuthCandidate{ID: "auth-ready", Provider: providerCodex},
	)
	response, err := handleSchedulerPick(raw)
	if err != nil {
		t.Fatalf("handleSchedulerPick() error = %v", err)
	}

	var envelope pluginabi.Envelope
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response error = %#v", envelope.Error)
	}
	var decision pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(envelope.Result, &decision); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	if !decision.Handled || decision.AuthID != "auth-ready" {
		t.Fatalf("decision = %#v, want auth-ready", decision)
	}
}

func TestHandleSchedulerPickNoBansDefersToHostRouting(t *testing.T) {
	restoreBans := replaceBansForTest(map[string]banEntry{
		"auth-elsewhere": {ResetAt: time.Now().Add(30 * time.Minute), Window: "5h"},
	})
	t.Cleanup(restoreBans)

	raw := schedulerPickRequest(t,
		pluginapi.SchedulerAuthCandidate{ID: "auth-a", Provider: providerCodex},
		pluginapi.SchedulerAuthCandidate{ID: "auth-b", Provider: providerCodex},
		pluginapi.SchedulerAuthCandidate{ID: "auth-c", Provider: "claude"},
	)
	response, err := handleSchedulerPick(raw)
	if err != nil {
		t.Fatalf("handleSchedulerPick() error = %v", err)
	}

	var envelope pluginabi.Envelope
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.OK {
		t.Fatalf("response error = %#v", envelope.Error)
	}
	var decision pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(envelope.Result, &decision); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	if decision.Handled || decision.Reject || decision.AuthID != "" || decision.DelegateBuiltin != "" {
		t.Fatalf("decision = %#v, want Handled=false so the host keeps its routing strategy and session affinity", decision)
	}
}

func TestPluginRegistrationReceivesCandidatesAcrossPriorities(t *testing.T) {
	if !pluginRegistration().Capabilities.SchedulerAcrossPriorities {
		t.Fatal("SchedulerAcrossPriorities = false, want true")
	}
}

func TestBanStateRestoresOnlyActiveEntries(t *testing.T) {
	t.Setenv(banStateFileEnv, filepath.Join(t.TempDir(), "bans.json"))
	now := time.Now()
	active := banEntry{ResetAt: now.Add(30 * time.Minute), Window: "5h", BannedAt: now}
	expired := banEntry{ResetAt: now.Add(-time.Minute), Window: "5h", BannedAt: now.Add(-2 * time.Minute)}

	writer := banState{}
	writer.set("auth-active", active)
	writer.set("auth-expired", expired)

	restored := banState{}
	restored.load(now)
	got, ok := restored.lookup("auth-active")
	if !ok || !got.ResetAt.Equal(active.ResetAt) || got.Window != active.Window {
		t.Fatalf("restored active ban = %#v, %t; want %#v, true", got, ok, active)
	}
	if _, ok := restored.lookup("auth-expired"); ok {
		t.Fatal("expired ban was restored")
	}
}

func schedulerPickRequest(t *testing.T, candidates ...pluginapi.SchedulerAuthCandidate) []byte {
	t.Helper()
	raw, err := json.Marshal(pluginapi.SchedulerPickRequest{Candidates: candidates})
	if err != nil {
		t.Fatalf("marshal scheduler request: %v", err)
	}
	return raw
}

func replaceBansForTest(bans map[string]banEntry) func() {
	original := banStore.snapshot()
	banStore = banState{bans: bans}
	return func() {
		banStore = banState{bans: original}
	}
}
