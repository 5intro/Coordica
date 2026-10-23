package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"coordica/internal/domain"
	"coordica/internal/gitops"
	"coordica/internal/store"
)

type AcceptanceSignoffs struct{ Service *Service }

func signoffKey(id, commit string) string { return "signoff_" + Hash(id+"\x00"+commit) }

func (s AcceptanceSignoffs) Read(ctx context.Context, a Actor, id, commit string) (domain.AcceptanceSignoffStatus, error) {
	var out domain.AcceptanceSignoffStatus
	if err := owner(a); err != nil {
		return out, err
	}
	oid, err := gitops.ParseOID(commit)
	if err != nil || !domain.ChatIdentifier.MatchString(id) {
		return out, domain.Fail("invalid_input", "需要有效需求和完整固定提交")
	}
	err = s.Service.DB.View(ctx, func(tx *store.Tx) error {
		coverage, err := acceptanceCoverage(tx, id, string(oid))
		if err != nil {
			return err
		}
		out = domain.AcceptanceSignoffStatus{RequirementID: id, Commit: string(oid), Digest: coverage.Digest,
			CanAccept: signoffReady(coverage)}
		old, err := readSignoff(tx, signoffKey(id, string(oid)))
		if err != nil {
			return err
		}
		if old.Version > 0 {
			out.Receipt = &old
			out.Accepted = out.CanAccept && old.State == "accepted" && old.CoverageDigest == coverage.Digest &&
				old.Revision == coverage.Plan.Revision && old.PlanVersion == coverage.Plan.Version
		}
		out.Recent, err = store.RecentAcceptanceSignoffs(tx, signoffKey(id, string(oid)))
		return err
	})
	return out, err
}

func (s AcceptanceSignoffs) Decide(ctx context.Context, a Actor, key string, in domain.AcceptanceSignoffRequest) (json.RawMessage, error) {
	if err := owner(a); err != nil {
		return nil, err
	}
	if err := domain.ValidateAcceptanceSignoffRequest(in); err != nil {
		return nil, err
	}
	oid, err := gitops.ParseOID(in.Commit)
	if err != nil {
		return nil, domain.Fail("invalid_input", "需要完整的固定提交 SHA")
	}
	if raw, found, err := readMutationReplay(ctx, s.Service, a, key, "tests.acceptance.signoff", in); err != nil || found {
		return raw, err
	}
	if in.Action == "accept" {
		if err := requireManagedCommit(ctx, s.Service, oid); err != nil {
			return nil, err
		}
	}
	return s.Service.mutate(ctx, a, key, "tests.acceptance.signoff", in, func(tx *store.Tx) (any, error) {
		return s.apply(tx, a, in, string(oid))
	})
}

func (s AcceptanceSignoffs) apply(tx *store.Tx, a Actor, in domain.AcceptanceSignoffRequest, commit string) (any, error) {
	id := signoffKey(in.RequirementID, commit)
	old, err := readSignoff(tx, id)
	if err != nil {
		return nil, err
	}
	if err := version(old.Version, in.ExpectedVersion); err != nil {
		return nil, err
	}
	value := old
	if in.Action == "accept" {
		coverage, err := acceptanceCoverage(tx, in.RequirementID, commit)
		if err != nil {
			return nil, err
		}
		if err := validateSignoffCoverage(coverage, in); err != nil {
			return nil, err
		}
		value = domain.AcceptanceSignoff{ID: id, RequirementID: in.RequirementID, Commit: commit,
			Revision: coverage.Plan.Revision, PlanVersion: coverage.Plan.Version, CoverageDigest: coverage.Digest,
			ManualCriteria: slices.Clone(in.ManualCriteria), State: "accepted"}
	} else {
		if old.State != "accepted" {
			return nil, domain.Fail("conflict", "没有可撤销的签收")
		}
		value.State = "revoked"
	}
	value.Version, value.Note, value.ActorID, value.RecordedAt = old.Version+1, in.Note, a.ID, s.Service.Now()
	if err := tx.Put("acceptance_signoff", id, value); err != nil {
		return nil, err
	}
	if err := tx.Put("acceptance_signoff_revision", fmt.Sprintf("%s_v%020d", id, value.Version), value); err != nil {
		return nil, err
	}
	return value, s.Service.audit(tx, a, "tests.acceptance."+value.State, in.RequirementID, fmt.Sprint(value.Version))
}

func readSignoff(tx *store.Tx, key string) (domain.AcceptanceSignoff, error) {
	var value domain.AcceptanceSignoff
	err := tx.Get("acceptance_signoff", key, &value)
	if errors.Is(err, domain.ErrNotFound) {
		err = nil
	}
	return value, err
}

func signoffReady(c domain.AcceptanceCoverage) bool {
	if !c.Current || len(c.Criteria) == 0 {
		return false
	}
	for _, item := range c.Criteria {
		if item.Criterion.Method != "manual" && (item.Criterion.Method != "automated" || item.State != "passed") {
			return false
		}
	}
	return true
}

func validateSignoffCoverage(c domain.AcceptanceCoverage, in domain.AcceptanceSignoffRequest) error {
	if !c.Current || c.Digest != in.ExpectedDigest {
		return domain.Fail("version_conflict", "需求、验收条款或测试证据已变化，请重新核对")
	}
	if !signoffReady(c) {
		return domain.Fail("conflict", "自动测试证据尚未齐备")
	}
	manual := []string{}
	for _, item := range c.Criteria {
		if item.Criterion.Method == "manual" {
			manual = append(manual, item.Criterion.ID)
		}
	}
	got := slices.Clone(in.ManualCriteria)
	slices.Sort(got)
	slices.Sort(manual)
	if !slices.Equal(got, manual) {
		return domain.Fail("invalid_input", "必须逐项确认当前全部人工验收条款")
	}
	return nil
}
