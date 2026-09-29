package ui

import (
	"image/color"
	"strings"

	"github.com/prepublish/prepublish-cli/internal/api"
	"github.com/prepublish/prepublish-cli/internal/config"
)

// RenderAccount is `prepublish whoami`: who the credential belongs to, which
// plan it is on, how much of today's allowance is left, and where the
// credential came from. The last one matters more than it looks — a token from
// PREPUBLISH_API_KEY behaves differently from a saved key (a command that
// appears to log out does nothing), and the card is where that becomes visible.
//
// Every argument is optional: an anonymous caller arrives as (nil, nil, free,
// SourceNone), a signed-in one as (user, usage, nil, SourceFile|SourceEnv).
func RenderAccount(u *api.User, usage *api.Usage, free *api.CheckFree, source config.CredentialSource, width int) string {
	w := contentWidth(width)
	inner := boxInner(w)
	tier := accountTier(u, usage, free)

	identity := "Anonymous"
	if u != nil {
		if email := strings.TrimSpace(u.Email); email != "" {
			identity = email
		} else {
			identity = "Signed in"
		}
	}

	var body []string
	body = append(body, st.h1.Render(truncate(identity, inner)))

	// The plan pill is only worth a pill when it says something the identity
	// does not: an anonymous caller's heading already says Anonymous, and a
	// badge repeating it is noise.
	var badges []string
	if u != nil {
		badges = append(badges, badge(tierLabel(tier), tierColor(tier)))
		if u.SubscriptionStatus != "" {
			badges = append(badges, chip(u.SubscriptionStatus, subscriptionColor(u.SubscriptionStatus)))
		}
	}
	if quota := quotaLine(usage, free); quota != "" {
		badges = append(badges, st.dim.Render(quota))
	}
	if len(badges) > 0 {
		body = append(body, strings.Join(badges, "  "))
	}

	if q := quotaBar(usage, free, inner); q != "" {
		body = append(body, q)
	}

	var facts []string
	switch source {
	case config.SourceEnv:
		facts = append(facts, "key from PREPUBLISH_API_KEY, for this terminal only")
	case config.SourceFile:
		// Showing the file is a nicety: without a resolvable config directory
		// there is no path to name, and the card says nothing rather than
		// printing a broken one.
		if path, err := config.CredentialsPath(); err == nil {
			facts = append(facts, "key saved in "+path)
		}
	default:
		facts = append(facts, "no credential saved on this machine")
	}
	if u != nil && u.AnalysisCount > 0 {
		facts = append(facts, plural(u.AnalysisCount, "audit", "audits")+" run in total")
	}
	if free != nil && free.MaxFileSizeMB > 0 && (tier == api.TierPaid || tier == api.TierStudio) {
		facts = append(facts, "uploads up to "+count(free.MaxFileSizeMB)+" MB")
	}
	body = append(body, st.dim.Render(wrapBlock(strings.Join(facts, "\n"), inner)))

	card := box("Account", body, w)

	var tail []string
	switch {
	case u == nil && usage == nil:
		tail = append(tail, st.dim.Render(wrapBlock(
			"Anonymous audits are tied to this machine and its network. Signing in keeps them and raises the daily allowance.", w)))
		tail = append(tail, "  "+st.key.Render("prepublish login"))
	case tier == api.TierFreeAccount:
		tail = append(tail, st.dim.Render(wrapBlock(
			"Creator raises the daily allowance to 50 audits and unlocks full reports, uploads and thumbnails.", w)))
		tail = append(tail, "  "+st.key.Render("prepublish upgrade"))
	}
	if msg := freeMessage(free); msg != "" {
		tail = append(tail, st.dim.Render(wrapBlock(msg, w)))
	}
	return fit(stack("\n\n", card, strings.Join(tail, "\n")), w)
}

// accountTier prefers the account's own tier, then the anonymous read's, and
// falls back to free account because that is what a credential without a usage
// read means.
func accountTier(u *api.User, usage *api.Usage, free *api.CheckFree) string {
	switch {
	case usage != nil && usage.Tier != "":
		return usage.Tier
	case free != nil && free.Tier != "":
		return free.Tier
	case u != nil:
		return api.TierFreeAccount
	default:
		return api.TierAnonymous
	}
}

func subscriptionColor(status string) color.Color {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return th.success
	case "past_due":
		return th.danger
	case "canceled":
		return th.warning
	default:
		return th.faint
	}
}

// quotaLine is the sentence the home screen and whoami share: "47 of 50 audits
// left today". It is the plain-language half; quotaBar is the visual half.
func quotaLine(usage *api.Usage, free *api.CheckFree) string {
	switch {
	case usage != nil && usage.AuditsLimit > 0:
		return itoa(usage.AuditsRemaining) + " of " + itoa(usage.AuditsLimit) + " audits left today"
	case usage != nil:
		return "no daily limit reported"
	case free != nil && free.Limit > 0:
		return itoa(free.Remaining) + " of " + itoa(free.Limit) + " audits left today"
	case free != nil:
		return itoa(free.Remaining) + " audits left today"
	default:
		return ""
	}
}

// quotaBar draws the used fraction of today's allowance. A bar earns its place
// here: this is the number that runs out, and it is the one a creator comes
// back to check.
func quotaBar(usage *api.Usage, free *api.CheckFree, w int) string {
	var used, limit int
	switch {
	case usage != nil && usage.AuditsLimit > 0:
		used, limit = usage.AuditsUsedToday, usage.AuditsLimit
	case free != nil && free.Limit > 0:
		limit = free.Limit
		used = limit - free.Remaining
	default:
		return ""
	}
	used = clampInt(used, 0, limit)
	pct := used * 100 / max(limit, 1)
	barW := clampInt(w-8, 8, 44)
	return "  " + barWith(pct, th.accent, barW) + " " + st.faint.Render(itoa(used)+"/"+itoa(limit)+" used today")
}

func freeMessage(free *api.CheckFree) string {
	if free == nil {
		return ""
	}
	return strings.TrimSpace(free.Message)
}
