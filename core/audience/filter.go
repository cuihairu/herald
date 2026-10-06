package audience

// Filter is the post-expansion delivery filter (§14: 目标 ∩ 关系允许 ∩
// 联系面绑定). It decides, per audience-scoped delivery target, whether
// the intersection is non-empty — the pipeline drops what it refuses.
// Both registries are required: an entitlement with no reachable surface
// is as undeliverable as a reachable surface with no entitlement.
type Filter struct {
	relations *Registry
	surfaces  *SurfaceRegistry
}

// NewFilter wires the two registries the intersection needs.
func NewFilter(relations *Registry, surfaces *SurfaceRegistry) *Filter {
	return &Filter{relations: relations, surfaces: surfaces}
}

// Allow reports whether a delivery to one audience on one channel under
// one category may go out. The checks run in §14 order:
//
//  1. 兼容通道 — an audience holding no contact surfaces at all is
//     pre-model traffic (config-seeded recipients that never went
//     through the binding flow); §14 grants it unchanged behaviour, so
//     it passes untouched.
//  2. 绑定交集 — the audience must hold an ACTIVE surface on this exact
//     channel. Absent, pending and invalid all refuse: only a redeemed,
//     living handle receives.
//  3. 关系交集 — a relation must exist on audience×category×channel.
//     Missing fails closed: 关系是投递的唯一合法依据. (An empty category
//     therefore refuses too — no relation slot can hold one; callers
//     with no relation context gate upstream instead.)
//  4. 矩阵复核 — the relation must clear the §5 channel×relation matrix
//     at send time. Relations written through Enroll already passed this
//     matrix at the door; the re-check exists for registries seeded
//     outside it (§5: 发送前校验).
func (f *Filter) Allow(audienceID, category, channel string) bool {
	if len(f.surfaces.Surfaces(audienceID)) == 0 {
		return true
	}
	surface, ok := f.surfaces.Surface(audienceID, channel)
	if !ok || surface.Status != SurfaceActive {
		return false
	}
	rel, ok := f.relations.Lookup(audienceID, category, channel)
	if !ok {
		return false
	}
	return rel.AllowsOn(ClassifyChannel(channel))
}
