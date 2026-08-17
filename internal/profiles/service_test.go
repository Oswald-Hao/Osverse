package profiles

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestServiceRequiresSuccessfulProbeAndAppliesConfirmedTargets(t *testing.T) {
	home := t.TempDir()
	store, _ := NewStore(home)
	adapters, _ := NewAdapterSet(home)
	prober := &fakeProfileProber{result: compatibleProbe()}
	service := testProfileService(store, prober, adapters)
	createInput := adapterInput()
	createInput.ID = ""
	profile, err := service.Save(context.Background(), createInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateApplyPlan(context.Background(), profile.ID, []string{TargetCodex}); !errors.Is(err, ErrProbeRequired) {
		t.Fatalf("unprobed plan error = %v", err)
	}

	probe, err := service.Probe(context.Background(), profile.ID)
	if err != nil || !probe.Authenticated || prober.got.APIKey != "secret-key-1234" {
		t.Fatalf("Probe() = (%#v, %v), secret %#v", probe, err, prober.got)
	}
	matrix, err := service.Compatibility(profile.ID)
	if err != nil || len(matrix) != 6 {
		t.Fatalf("Compatibility() = (%#v, %v)", matrix, err)
	}
	for _, item := range matrix {
		if !item.Compatible {
			t.Errorf("target compatibility = %#v", item)
		}
	}
	plan, err := service.CreateApplyPlan(context.Background(), profile.ID, []string{TargetOpenCode, TargetClaude, TargetCodex, TargetQwen, TargetHarness, TargetKimi})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ProfileName != "Work" || plan.KeyHint != "••••1234" || len(plan.Effects) != 6 || plan.Warning == "" {
		t.Fatalf("plan = %#v", plan)
	}
	wantOrder := []string{TargetClaude, TargetCodex, TargetHarness, TargetKimi, TargetOpenCode, TargetQwen}
	gotOrder := []string{plan.Effects[0].Target, plan.Effects[1].Target, plan.Effects[2].Target, plan.Effects[3].Target, plan.Effects[4].Target, plan.Effects[5].Target}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Fatalf("effect order = %#v", gotOrder)
	}

	result, err := service.Apply(context.Background(), plan.ID)
	if err != nil || result.Succeeded != 6 || result.Failed != 0 || len(result.Results) != 6 {
		t.Fatalf("Apply() = (%#v, %v)", result, err)
	}
	for _, target := range wantOrder {
		path, _ := adapters.TargetPath(target)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Errorf("target %s config = (%v, %v)", target, info, err)
		}
	}
	if _, err := service.Apply(context.Background(), plan.ID); !errors.Is(err, ErrApplyPlan) {
		t.Fatalf("reused apply plan error = %v", err)
	}
}

func TestHarnessCompatibilityUsesPreferredConfirmedProtocol(t *testing.T) {
	store := &fakeProfileStore{
		profiles: []Profile{{ID: "profile", Name: "P", KeyHint: "••••1234"}},
		secret:   adapterInput(),
	}
	adapters := &fakeProfileAdapters{}
	probe := compatibleProbe()
	service := testProfileService(store, &fakeProfileProber{result: probe}, adapters)
	if _, err := service.Probe(context.Background(), "profile"); err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreateApplyPlan(context.Background(), "profile", []string{TargetHarness})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), plan.ID); err != nil {
		t.Fatal(err)
	}
	if adapters.protocols[TargetHarness] != "openai-chat" {
		t.Fatalf("Harness protocol = %q", adapters.protocols[TargetHarness])
	}

	probe.Protocols[1].Status = "unavailable"
	service = testProfileService(store, &fakeProfileProber{result: probe}, adapters)
	_, _ = service.Probe(context.Background(), "profile")
	plan, err = service.CreateApplyPlan(context.Background(), "profile", []string{TargetHarness})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = service.Apply(context.Background(), plan.ID)
	if adapters.protocols[TargetHarness] != "openai-responses" {
		t.Fatalf("Harness fallback protocol = %q", adapters.protocols[TargetHarness])
	}
}

func TestKimiCompatibilityUsesPreferredConfirmedProtocol(t *testing.T) {
	store := &fakeProfileStore{
		profiles: []Profile{{ID: "profile", Name: "P", KeyHint: "••••1234"}},
		secret:   adapterInput(),
	}
	adapters := &fakeProfileAdapters{}
	probe := compatibleProbe()
	service := testProfileService(store, &fakeProfileProber{result: probe}, adapters)
	if _, err := service.Probe(context.Background(), "profile"); err != nil {
		t.Fatal(err)
	}
	plan, err := service.CreateApplyPlan(context.Background(), "profile", []string{TargetKimi})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Apply(context.Background(), plan.ID); err != nil {
		t.Fatal(err)
	}
	if adapters.protocols[TargetKimi] != "openai-chat" {
		t.Fatalf("Kimi protocol = %q", adapters.protocols[TargetKimi])
	}
}

func TestHarnessCompatibilityRejectsUnconfirmedProtocols(t *testing.T) {
	store := &fakeProfileStore{
		profiles: []Profile{{ID: "profile", Name: "P", KeyHint: "••••1234"}},
		secret:   adapterInput(),
	}
	probe := compatibleProbe()
	for index := range probe.Protocols {
		probe.Protocols[index].Status = "unavailable"
	}
	service := testProfileService(store, &fakeProfileProber{result: probe}, &fakeProfileAdapters{})
	if _, err := service.Probe(context.Background(), "profile"); err != nil {
		t.Fatal(err)
	}
	matrix, err := service.Compatibility("profile")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range matrix {
		if item.Target == TargetHarness && item.Compatible {
			t.Fatalf("Harness unexpectedly compatible: %#v", item)
		}
	}
	if _, err := service.CreateApplyPlan(context.Background(), "profile", []string{TargetHarness}); !errors.Is(err, ErrProbeRequired) {
		t.Fatalf("unconfirmed Harness plan error = %v", err)
	}
}

func TestServiceCompatibilityBlocksMismatchedTargets(t *testing.T) {
	home := t.TempDir()
	store, _ := NewStore(home)
	adapters, _ := NewAdapterSet(home)
	probe := compatibleProbe()
	probe.Protocols[0].Status = "unavailable"
	service := testProfileService(store, &fakeProfileProber{result: probe}, adapters)
	createInput := adapterInput()
	createInput.ID = ""
	profile, _ := service.Save(context.Background(), createInput)
	if _, err := service.Probe(context.Background(), profile.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateApplyPlan(context.Background(), profile.ID, []string{TargetCodex}); !errors.Is(err, ErrProbeRequired) {
		t.Fatalf("incompatible Codex plan error = %v", err)
	}
	if _, err := service.CreateApplyPlan(context.Background(), profile.ID, []string{TargetClaude}); err != nil {
		t.Fatalf("compatible Claude plan error = %v", err)
	}
}

func TestCompatibilityUsesChatCompletionsForOpenCode(t *testing.T) {
	probe := compatibleProbe()
	probe.Protocols[0].Status = "unavailable"
	matrix := compatibilityMatrix(probe)
	compatible := make(map[string]bool, len(matrix))
	for _, item := range matrix {
		compatible[item.Target] = item.Compatible
	}
	if compatible[TargetCodex] || !compatible[TargetOpenCode] {
		t.Fatalf("responses unavailable compatibility = %#v", compatible)
	}

	probe.Protocols[0].Status = "compatible"
	probe.Protocols[1].Status = "unavailable"
	matrix = compatibilityMatrix(probe)
	for _, item := range matrix {
		compatible[item.Target] = item.Compatible
	}
	if !compatible[TargetCodex] || compatible[TargetOpenCode] {
		t.Fatalf("chat unavailable compatibility = %#v", compatible)
	}
}

func TestProfileUpdateAndDeleteInvalidateObservationsAndPlans(t *testing.T) {
	home := t.TempDir()
	store, _ := NewStore(home)
	adapters, _ := NewAdapterSet(home)
	service := testProfileService(store, &fakeProfileProber{result: compatibleProbe()}, adapters)
	createInput := adapterInput()
	createInput.ID = ""
	profile, _ := service.Save(context.Background(), createInput)
	_, _ = service.Probe(context.Background(), profile.ID)
	plan, _ := service.CreateApplyPlan(context.Background(), profile.ID, []string{TargetClaude})
	updated := adapterInput()
	updated.ID = profile.ID
	updated.Model = "other-model"
	if _, err := service.Save(context.Background(), updated); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Compatibility(profile.ID); !errors.Is(err, ErrProbeRequired) {
		t.Fatalf("stale compatibility error = %v", err)
	}
	if _, err := service.Apply(context.Background(), plan.ID); !errors.Is(err, ErrApplyPlan) {
		t.Fatalf("stale plan apply error = %v", err)
	}
	if err := service.Delete(context.Background(), profile.ID); err != nil {
		t.Fatal(err)
	}
	listed, _ := service.List(context.Background())
	if len(listed) != 0 {
		t.Fatalf("profiles after delete = %#v", listed)
	}
}

func TestApplyContinuesAfterOneTargetFailsWithoutLeakingError(t *testing.T) {
	store := &fakeProfileStore{
		profiles: []Profile{{ID: "profile", Name: "P", KeyHint: "••••1234"}},
		secret:   adapterInput(),
	}
	adapters := &fakeProfileAdapters{}
	service := testProfileService(store, &fakeProfileProber{result: compatibleProbe()}, adapters)
	_, _ = service.Probe(context.Background(), "profile")
	plan, err := service.CreateApplyPlan(context.Background(), "profile", []string{TargetClaude, TargetCodex})
	if err != nil {
		t.Fatal(err)
	}
	adapters.fail = TargetClaude
	result, err := service.Apply(context.Background(), plan.ID)
	if err != nil || result.Succeeded != 1 || result.Failed != 1 {
		t.Fatalf("Apply() = (%#v, %v)", result, err)
	}
	if result.Results[0].Target != TargetClaude || result.Results[0].Applied || result.Results[0].Path != "" {
		t.Fatalf("redacted failed result = %#v", result.Results[0])
	}
}

func TestApplyDoesNotClaimHarnessFilesWerePreservedAfterRollbackFailure(t *testing.T) {
	store := &fakeProfileStore{
		profiles: []Profile{{ID: "profile", Name: "P", KeyHint: "••••1234"}},
		secret:   adapterInput(),
	}
	adapters := &fakeProfileAdapters{fail: TargetHarness, failErr: ErrConfigRollback}
	service := testProfileService(store, &fakeProfileProber{result: compatibleProbe()}, adapters)
	_, _ = service.Probe(context.Background(), "profile")
	plan, err := service.CreateApplyPlan(context.Background(), "profile", []string{TargetHarness})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Apply(context.Background(), plan.ID)
	if err != nil || result.Failed != 1 || len(result.Results) != 1 {
		t.Fatalf("Apply() = (%#v, %v)", result, err)
	}
	if strings.Contains(result.Results[0].Message, "原文件已保留") || !strings.Contains(result.Results[0].Message, "回滚未完成") {
		t.Fatalf("rollback result message = %q", result.Results[0].Message)
	}
}

type fakeProfileProber struct {
	result ProbeResult
	err    error
	got    Input
}

func (prober *fakeProfileProber) Probe(_ context.Context, input Input) (ProbeResult, error) {
	prober.got = input
	return prober.result, prober.err
}

func compatibleProbe() ProbeResult {
	return ProbeResult{
		Reachable: true, Authenticated: true,
		Protocols: []ProtocolResult{
			{Protocol: "openai-responses", Status: "compatible"},
			{Protocol: "openai-chat", Status: "compatible"},
			{Protocol: "anthropic-messages", Status: "compatible"},
		},
	}
}

func testProfileService(store profileStore, prober profileProber, adapters profileAdapters) *Service {
	var sequence atomic.Uint64
	return newService(store, prober, adapters, func() time.Time {
		return time.Date(2026, time.August, 14, 1, 2, int(sequence.Load()), 0, time.UTC)
	}, func() (string, error) {
		return "id-" + strconv.FormatUint(sequence.Add(1), 10), nil
	})
}

type fakeProfileStore struct {
	profiles []Profile
	secret   Input
}

func (store *fakeProfileStore) Save(context.Context, Input) (Profile, error) { return Profile{}, nil }
func (store *fakeProfileStore) List(context.Context) ([]Profile, error) {
	return append([]Profile(nil), store.profiles...), nil
}
func (store *fakeProfileStore) Secret(context.Context, string) (Input, error) {
	return store.secret, nil
}
func (store *fakeProfileStore) Delete(context.Context, string) error { return nil }

type fakeProfileAdapters struct {
	fail      string
	failErr   error
	protocols map[string]string
}

func (adapters *fakeProfileAdapters) TargetPath(target string) (string, error) {
	return filepath.Join("/home/test", ".config", target), nil
}

func (adapters *fakeProfileAdapters) Apply(_ context.Context, target string, input Input) (ApplyResult, error) {
	if adapters.protocols == nil {
		adapters.protocols = make(map[string]string)
	}
	adapters.protocols[target] = input.Protocol
	if target == adapters.fail {
		if adapters.failErr != nil {
			return ApplyResult{}, adapters.failErr
		}
		return ApplyResult{}, errors.New("secret backend error")
	}
	return ApplyResult{Target: target, Applied: true, Path: "/safe/path", Message: "ok"}, nil
}
