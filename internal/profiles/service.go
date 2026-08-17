package profiles

import (
	"context"
	"crypto/rand"
	"errors"
	"sort"
	"sync"
	"time"
)

const applyPlanLifetime = 10 * time.Minute
const probeObservationLifetime = 10 * time.Minute

var (
	ErrProbeRequired = errors.New("compatible API probe required")
	ErrApplyPlan     = errors.New("API apply plan unavailable")
)

type profileStore interface {
	Save(context.Context, Input) (Profile, error)
	List(context.Context) ([]Profile, error)
	Secret(context.Context, string) (Input, error)
	Delete(context.Context, string) error
}

type profileProber interface {
	Probe(context.Context, Input) (ProbeResult, error)
}

type profileAdapters interface {
	TargetPath(string) (string, error)
	Apply(context.Context, string, Input) (ApplyResult, error)
}

// TargetCompatibility is the fixed matrix shown after a probe.
type TargetCompatibility struct {
	Target     string `json:"target"`
	Compatible bool   `json:"compatible"`
	Reason     string `json:"reason"`
}

// ApplyEffect is one target file disclosed before confirmation.
type ApplyEffect struct {
	Target      string `json:"target"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

// ApplyPlan is a single-use backend-owned configuration plan.
type ApplyPlan struct {
	ID          string        `json:"id"`
	ProfileID   string        `json:"profileId"`
	ProfileName string        `json:"profileName"`
	KeyHint     string        `json:"keyHint"`
	Effects     []ApplyEffect `json:"effects"`
	Warning     string        `json:"warning"`
	CreatedAt   time.Time     `json:"createdAt"`
	ExpiresAt   time.Time     `json:"expiresAt"`
}

// ApplyBatchResult reports each target independently.
type ApplyBatchResult struct {
	PlanID    string        `json:"planId"`
	ProfileID string        `json:"profileId"`
	Results   []ApplyResult `json:"results"`
	Succeeded int           `json:"succeeded"`
	Failed    int           `json:"failed"`
}

type storedApplyPlan struct {
	public    ApplyPlan
	targets   []string
	protocols map[string]string
	used      bool
}

type probeObservation struct {
	result ProbeResult
}

// Service coordinates encrypted storage, no-cost probes, and confirmed apply plans.
type Service struct {
	mu       sync.Mutex
	store    profileStore
	prober   profileProber
	adapters profileAdapters
	now      func() time.Time
	randomID func() (string, error)
	probes   map[string]probeObservation
	plans    map[string]*storedApplyPlan
}

func NewService(home string) (*Service, error) {
	store, err := NewStore(home)
	if err != nil {
		return nil, err
	}
	adapters, err := NewAdapterSet(home)
	if err != nil {
		return nil, err
	}
	return newService(store, NewProber(), adapters, time.Now, func() (string, error) { return randomID(rand.Reader) }), nil
}

func newService(store profileStore, prober profileProber, adapters profileAdapters, now func() time.Time, randomID func() (string, error)) *Service {
	if store == nil || prober == nil || adapters == nil || now == nil || randomID == nil {
		return nil
	}
	return &Service{
		store: store, prober: prober, adapters: adapters, now: now, randomID: randomID,
		probes: make(map[string]probeObservation), plans: make(map[string]*storedApplyPlan),
	}
}

func (service *Service) Save(ctx context.Context, input Input) (Profile, error) {
	if service == nil {
		return Profile{}, ErrUnsafeStorage
	}
	profile, err := service.store.Save(ctx, input)
	if err == nil {
		service.mu.Lock()
		delete(service.probes, profile.ID)
		service.invalidateProfilePlansLocked(profile.ID)
		service.mu.Unlock()
	}
	return profile, err
}

func (service *Service) List(ctx context.Context) ([]Profile, error) {
	if service == nil {
		return nil, ErrUnsafeStorage
	}
	return service.store.List(ctx)
}

func (service *Service) Delete(ctx context.Context, id string) error {
	if service == nil {
		return ErrUnsafeStorage
	}
	if err := service.store.Delete(ctx, id); err != nil {
		return err
	}
	service.mu.Lock()
	delete(service.probes, id)
	service.invalidateProfilePlansLocked(id)
	service.mu.Unlock()
	return nil
}

func (service *Service) Probe(ctx context.Context, id string) (ProbeResult, error) {
	if service == nil {
		return ProbeResult{}, ErrProbeFailed
	}
	secret, err := service.store.Secret(ctx, id)
	if err != nil {
		return ProbeResult{}, err
	}
	result, err := service.prober.Probe(ctx, secret)
	if err != nil {
		return ProbeResult{}, err
	}
	result.ProfileID = id
	result.CheckedAt = service.now().UTC()
	service.mu.Lock()
	service.probes[id] = probeObservation{result: cloneProbeResult(result)}
	service.invalidateProfilePlansLocked(id)
	service.mu.Unlock()
	return cloneProbeResult(result), nil
}

func (service *Service) Compatibility(profileID string) ([]TargetCompatibility, error) {
	if service == nil {
		return nil, ErrProbeRequired
	}
	service.mu.Lock()
	observation, ok := service.probes[profileID]
	service.mu.Unlock()
	if !ok {
		return nil, ErrProbeRequired
	}
	return compatibilityMatrix(observation.result), nil
}

func compatibilityMatrix(result ProbeResult) []TargetCompatibility {
	status := make(map[string]string, len(result.Protocols))
	for _, protocol := range result.Protocols {
		status[protocol.Protocol] = protocol.Status
	}
	checks := []struct {
		target   string
		protocol string
	}{
		{target: TargetClaude, protocol: "anthropic-messages"},
		{target: TargetCodex, protocol: "openai-responses"},
		{target: TargetOpenCode, protocol: "openai-chat"},
		{target: TargetQwen, protocol: "openai-chat"},
	}
	matrix := make([]TargetCompatibility, 0, len(checks)+1)
	for _, check := range checks {
		compatible := result.Authenticated && status[check.protocol] == "compatible"
		reason := "需要先通过凭据和协议探测"
		if result.Authenticated && status[check.protocol] != "compatible" {
			reason = "API 未确认所需协议"
		}
		if compatible {
			reason = "凭据和协议均已确认"
		}
		matrix = append(matrix, TargetCompatibility{Target: check.target, Compatible: compatible, Reason: reason})
	}
	harnessProtocol, compatible := preferredHarnessProtocol(result)
	reason := "需要先通过凭据和协议探测"
	if result.Authenticated && !compatible {
		reason = "API 未确认 Harness 支持的协议"
	}
	if compatible {
		reason = "将使用 " + protocolDisplayName(harnessProtocol)
	}
	matrix = append(matrix, TargetCompatibility{Target: TargetHarness, Compatible: compatible, Reason: reason})
	return matrix
}

func preferredHarnessProtocol(result ProbeResult) (string, bool) {
	if !result.Authenticated {
		return "", false
	}
	status := make(map[string]string, len(result.Protocols))
	for _, protocol := range result.Protocols {
		status[protocol.Protocol] = protocol.Status
	}
	for _, protocol := range []string{"openai-chat", "openai-responses", "anthropic-messages"} {
		if status[protocol] == "compatible" {
			return protocol, true
		}
	}
	return "", false
}

func protocolDisplayName(protocol string) string {
	switch protocol {
	case "openai-chat":
		return "OpenAI Chat Completions"
	case "openai-responses":
		return "OpenAI Responses"
	case "anthropic-messages":
		return "Anthropic Messages"
	default:
		return protocol
	}
}

func (service *Service) CreateApplyPlan(ctx context.Context, profileID string, targets []string) (ApplyPlan, error) {
	if service == nil || len(targets) == 0 || len(targets) > 5 {
		return ApplyPlan{}, ErrApplyPlan
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ApplyPlan{}, err
	}
	profiles, err := service.store.List(ctx)
	if err != nil {
		return ApplyPlan{}, err
	}
	var profile Profile
	found := false
	for _, candidate := range profiles {
		if candidate.ID == profileID {
			profile = candidate
			found = true
			break
		}
	}
	if !found {
		return ApplyPlan{}, ErrProfileMissing
	}
	service.mu.Lock()
	observation, probed := service.probes[profileID]
	service.mu.Unlock()
	if !probed || service.now().Sub(observation.result.CheckedAt) >= probeObservationLifetime {
		return ApplyPlan{}, ErrProbeRequired
	}
	matrix := compatibilityMatrix(observation.result)
	allowed := make(map[string]bool, len(matrix))
	for _, item := range matrix {
		allowed[item.Target] = item.Compatible
	}
	seen := make(map[string]struct{}, len(targets))
	protocols := make(map[string]string, len(targets))
	canonical := make([]string, 0, len(targets))
	effects := make([]ApplyEffect, 0, len(targets))
	for _, target := range targets {
		if !allowed[target] {
			return ApplyPlan{}, ErrProbeRequired
		}
		if _, duplicate := seen[target]; duplicate {
			return ApplyPlan{}, ErrApplyPlan
		}
		seen[target] = struct{}{}
		if target == TargetHarness {
			protocol, ok := preferredHarnessProtocol(observation.result)
			if !ok {
				return ApplyPlan{}, ErrProbeRequired
			}
			protocols[target] = protocol
		}
		path, err := service.adapters.TargetPath(target)
		if err != nil {
			return ApplyPlan{}, err
		}
		canonical = append(canonical, target)
		effects = append(effects, ApplyEffect{
			Target: target, Path: path,
			Description: "备份并原子更新该工具的 Osverse provider 字段",
		})
		if target == TargetHarness {
			effects[len(effects)-1].Description = "备份并事务式更新 Harness Provider、默认模型与只写凭据"
		}
	}
	sort.Strings(canonical)
	sort.Slice(effects, func(i, j int) bool { return effects[i].Target < effects[j].Target })
	id, err := service.randomID()
	if err != nil || id == "" {
		return ApplyPlan{}, ErrApplyPlan
	}
	created := service.now().UTC()
	plan := ApplyPlan{
		ID: id, ProfileID: profileID, ProfileName: profile.Name, KeyHint: profile.KeyHint,
		Effects:   effects,
		Warning:   "所选 CLI 的官方配置需要保存明文 API Key；目标文件和备份均强制为 0600。",
		CreatedAt: created, ExpiresAt: created.Add(applyPlanLifetime),
	}
	service.mu.Lock()
	service.plans[id] = &storedApplyPlan{
		public: cloneApplyPlan(plan), targets: append([]string(nil), canonical...), protocols: protocols,
	}
	service.mu.Unlock()
	return cloneApplyPlan(plan), nil
}

func (service *Service) Apply(ctx context.Context, planID string) (ApplyBatchResult, error) {
	if service == nil || planID == "" {
		return ApplyBatchResult{}, ErrApplyPlan
	}
	service.mu.Lock()
	stored, ok := service.plans[planID]
	if !ok || stored.used || !service.now().Before(stored.public.ExpiresAt) {
		service.mu.Unlock()
		return ApplyBatchResult{}, ErrApplyPlan
	}
	stored.used = true
	plan := cloneApplyPlan(stored.public)
	targets := append([]string(nil), stored.targets...)
	protocols := make(map[string]string, len(stored.protocols))
	for target, protocol := range stored.protocols {
		protocols[target] = protocol
	}
	service.mu.Unlock()
	secret, err := service.store.Secret(ctx, plan.ProfileID)
	if err != nil {
		return ApplyBatchResult{}, err
	}
	result := ApplyBatchResult{PlanID: planID, ProfileID: plan.ProfileID, Results: make([]ApplyResult, 0, len(targets))}
	for _, target := range targets {
		if ctx != nil && ctx.Err() != nil {
			return result, ctx.Err()
		}
		secret.Protocol = protocols[target]
		applied, err := service.adapters.Apply(ctx, target, secret)
		if err != nil {
			message := "配置更新失败，原文件已保留"
			if errors.Is(err, ErrConfigRollback) {
				message = "配置更新失败且自动回滚未完成，请检查 Harness 设置和凭据文件"
			}
			applied = ApplyResult{Target: target, Applied: false, Message: message}
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Results = append(result.Results, applied)
	}
	return result, nil
}

func (service *Service) invalidateProfilePlansLocked(profileID string) {
	for id, plan := range service.plans {
		if plan.public.ProfileID == profileID {
			delete(service.plans, id)
		}
	}
}

func cloneProbeResult(result ProbeResult) ProbeResult {
	result.Protocols = append([]ProtocolResult(nil), result.Protocols...)
	return result
}

func cloneApplyPlan(plan ApplyPlan) ApplyPlan {
	plan.Effects = append([]ApplyEffect(nil), plan.Effects...)
	return plan
}
