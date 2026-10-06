package audience

import "strings"

// ChannelClass is the §5 channel family a channel name belongs to. The
// channel×relation matrix speaks in families, not raw provider names —
// "may a marketing enrollment use this channel" is a question about the
// channel's intrusiveness class, not about which provider implements it.
type ChannelClass string

const (
	// ChannelInstant: 秒级推送与 IM — telegram/feishu/dingtalk/短信/推送.
	ChannelInstant ChannelClass = "instant"
	// ChannelEmail: 邮件（SMTP）.
	ChannelEmail ChannelClass = "email"
	// ChannelApp: 应用侧 — webhook→站内信.
	ChannelApp ChannelClass = "app"
	// ChannelRSS: 拉式 feed（§9）.
	ChannelRSS ChannelClass = "rss"
)

// ClassifyChannel maps a channel name to its §5 family. Unknown names
// classify as instant — the strictest family for 指派·营销 (an unproven
// channel is never assumed to be a gentle one; a wrong guess refuses
// delivery instead of loosening it).
func ClassifyChannel(name string) ChannelClass {
	switch strings.ToLower(name) {
	case "rss", "feed":
		return ChannelRSS
	case "email", "smtp", "mail":
		return ChannelEmail
	case "webhook", "inbox":
		return ChannelApp
	default:
		return ChannelInstant
	}
}

// MatrixAllows is the §5 渠道×关系矩阵 as one decision:
//
//	订阅型            — 用户自选，四个族全开（RSS 仅订阅型，本行即其唯一入口）;
//	指派·系统必达     — 即时/邮件/应用侧多渠道并行保必达，RSS 无从拉起 → 拒;
//	指派·营销/运营    — 只许低打扰的邮件与应用侧，即时类轰炸禁用、RSS 拒.
//
// The policy bits pick the enrollment row: MustDeliver is the 系统必达
// row, its unsubscribable twin the 营销/运营 row (Enroll's §4 bottom
// lines guarantee every enrollment is exactly one of the two).
func MatrixAllows(relType RelationType, policy Policy, class ChannelClass) bool {
	switch relType {
	case RelationSubscription:
		return true
	case RelationEnrollment:
		if policy.MustDeliver {
			return class != ChannelRSS
		}
		return class == ChannelEmail || class == ChannelApp
	default:
		return false
	}
}

// AllowsOn is this relation's answer for one channel family — the §5
// matrix evaluated on the relation itself, used by the delivery filter
// as the send-time re-check (§5: 发送前校验) for registries seeded
// outside the Enroll door.
func (r Relation) AllowsOn(class ChannelClass) bool {
	return MatrixAllows(r.Type, r.Policy, class)
}
