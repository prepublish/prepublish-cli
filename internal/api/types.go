package api

import (
	"encoding/json"
	"time"
)

// This file mirrors the API's DTOs field for field, including the json tags and
// the omitempty markers. It is a wire contract, not a model: field names and
// types match the API exactly, so that a renamed or retyped field shows up as a
// compile error here instead of a silently empty value at runtime. The API's
// pointer-vs-value choices are followed where the difference is observable (a
// field that may be absent reads back as a pointer or a RawMessage); where ""
// and absent mean the same thing to every consumer, the value form is kept.

// Analysis status values. Terminal states are StatusCompleted and StatusFailed;
// everything else means a worker is still moving the job forward.
const (
	StatusPending           = "pending"
	StatusTranscribing      = "transcribing"
	StatusAnalyzing         = "analyzing"
	StatusComputingSaliency = "computing_saliency"
	StatusCompleted         = "completed"
	StatusFailed            = "failed"
)

// Tier values reported by the API for the caller.
const (
	TierAnonymous   = "anonymous"
	TierFreeAccount = "free_account"
	TierPaid        = "paid"
	TierStudio      = "studio"
)

// Lock reasons on a gated report.
const (
	LockReasonSignupRequired  = "signup_required"
	LockReasonUpgradeRequired = "upgrade_required"
)

// User is the account behind a credential (dto.UserDTO). It carries the Polar
// subscription status but not the plan tier; tier comes from Usage.
type User struct {
	ID                 string    `json:"id"`
	Email              string    `json:"email"`
	SubscriptionStatus string    `json:"subscription_status"` // inactive | active | canceled | past_due
	SubscriptionID     string    `json:"subscription_id,omitempty"`
	AnalysisCount      int       `json:"analysis_count"`
	CreatedAt          time.Time `json:"created_at"`
}

// Usage is one day of audit quota for the signed-in account
// (dto.UsageResponse). AuditsRemaining is derived server-side from the same
// count the analyze gate enforces.
type Usage struct {
	Tier            string `json:"tier"` // free_account | paid | studio
	AuditsUsedToday int    `json:"audits_used_today"`
	AuditsLimit     int    `json:"audits_limit"`
	AuditsRemaining int    `json:"audits_remaining"`
}

// CheckFree is the anonymous quota read (dto.CheckFreeUsageResponse). The route
// has no auth middleware, so this always reports the IP view and never an
// account's; use Usage for a signed-in caller.
type CheckFree struct {
	CanUseFree    bool   `json:"can_use_free"`
	Remaining     int    `json:"remaining"`
	Limit         int    `json:"limit,omitempty"`
	Tier          string `json:"tier,omitempty"`
	Message       string `json:"message,omitempty"`
	MaxFileSizeMB int64  `json:"max_file_size_mb"`
}

// AnalyzeRequest is the body of POST /api/analyze (dto.AnalyzeRequest).
//
// Email is required for a non-paid caller and ignored otherwise. ScriptText is
// required unless UploadID is set, which is why it is omitempty: with an upload
// the server reads the transcript from the uploaded file.
type AnalyzeRequest struct {
	VideoTitle      string `json:"video_title"`
	ScriptText      string `json:"script_text,omitempty"`
	VideoDuration   *int   `json:"video_duration,omitempty"` // seconds
	AnonymousUserID string `json:"anonymous_user_id,omitempty"`
	Category        string `json:"category,omitempty"`
	Audience        string `json:"audience,omitempty"`
	Email           string `json:"email,omitempty"`
	UploadID        string `json:"upload_id,omitempty"`
}

// Analysis is one audit report (dto.AnalyzeResponse).
//
// The same struct serves a pending poll, a failed run and a finished report.
// Which fields are populated follows Status, and how much of the report is
// populated follows the caller's entitlement: for an anonymous or free caller
// the server strips the rewrites, the title rewrite, the suggestions and the
// policy passages, marks the stripped improvements Locked, and sets Locked with
// a LockReason on the report itself.
type Analysis struct {
	ID            string  `json:"id"`
	VideoTitle    string  `json:"video_title"`
	ScriptText    string  `json:"script_text,omitempty"`
	VideoDuration *int    `json:"video_duration,omitempty"` // seconds
	ThumbnailPath string  `json:"thumbnail_path,omitempty"`
	Status        string  `json:"status"`
	Progress      int     `json:"progress"` // 0-100
	ErrorMessage  *string `json:"error_message,omitempty"`

	OverallScore   int  `json:"overall_score"`
	HookScore      *int `json:"hook_score,omitempty"`
	StructureScore *int `json:"structure_score,omitempty"`
	PacingScore    *int `json:"pacing_score,omitempty"`

	HookAnalysis      *HookAnalysis      `json:"hook_analysis,omitempty"`
	StructureAnalysis *StructureAnalysis `json:"structure_analysis,omitempty"`
	PacingAnalysis    *PacingAnalysis    `json:"pacing_analysis,omitempty"`
	RetentionCurve    []RetentionPoint   `json:"retention_curve"`
	// RetentionCurveSource is "model" when the curve came from the AI response
	// and "interpolated" when the backend generated or extended it. Absent on
	// rows that predate the field.
	RetentionCurveSource string `json:"retention_curve_source,omitempty"`

	// PreviousAnalysisID ties an anchored re-audit to the earlier draft's
	// audit; absent on a first audit.
	PreviousAnalysisID string           `json:"previous_analysis_id,omitempty"`
	RevisionSummary    *RevisionSummary `json:"revision_summary,omitempty"`
	ReadyToRecord      bool             `json:"ready_to_record"`

	PolicyPreflight   *PolicyPreflight   `json:"policy_preflight,omitempty"`
	Improvements      []Improvement      `json:"improvements"`
	OneKeyImprovement string             `json:"one_key_improvement"`
	VisualInsights    *VisualInsights    `json:"visual_insights,omitempty"`
	Authenticity      *Authenticity      `json:"authenticity,omitempty"`
	ScoreExplanations *ScoreExplanations `json:"score_explanations,omitempty"`
	TitleRewrite      *TitleRewrite      `json:"title_rewrite,omitempty"`
	// ChannelComparison is present for paid viewers only.
	ChannelComparison *ChannelComparison `json:"channel_comparison,omitempty"`
	// XrayEvidence is the Script X-ray replay model's ranking, paid only. Its
	// shape is owned by the model pipeline and changes with it, so it is passed
	// through raw rather than mirrored here.
	XrayEvidence     json.RawMessage `json:"xray_evidence,omitempty"`
	ProcessingTimeMs *int            `json:"processing_time_ms,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`

	// Locked reports that premium content was stripped server-side.
	Locked     bool   `json:"locked,omitempty"`
	LockReason string `json:"lock_reason,omitempty"` // signup_required | upgrade_required
	Tier       string `json:"tier,omitempty"`
}

// Terminal reports whether the audit has stopped moving. A terminal audit is
// what WaitAnalysis returns; a non-terminal one still has a worker behind it.
func (a *Analysis) Terminal() bool {
	return a.Status == StatusCompleted || a.Status == StatusFailed
}

// RevisionSummary is the draft chain's history (dto.RevisionSummaryDTO). It is
// context, not premium content, so gating never strips it.
type RevisionSummary struct {
	DraftNumber     int                 `json:"draft_number"`
	FirstAnalysisID string              `json:"first_analysis_id"`
	FirstScore      int                 `json:"first_score"`
	PreviousScore   int                 `json:"previous_score"`
	Implemented     []ImplementedAdvice `json:"implemented"`
	ResolvedCount   int                 `json:"resolved_count"`
}

// ImplementedAdvice is one earlier draft's advice that a later draft acted on.
type ImplementedAdvice struct {
	Title    string `json:"title"`
	Priority string `json:"priority"`
	Draft    int    `json:"draft"`
}

// ScoreExplanations explains why each component score was given.
type ScoreExplanations struct {
	Hook      string `json:"hook,omitempty"`
	Structure string `json:"structure,omitempty"`
	Pacing    string `json:"pacing,omitempty"`
}

// TitleRewrite is the suggested title improvement. Paid only.
type TitleRewrite struct {
	Original string `json:"original"`
	Improved string `json:"improved"`
	Why      string `json:"why,omitempty"`
}

// ChannelComparison is the conditioned-audit "vs. your channel's winners"
// section. Paid only.
type ChannelComparison struct {
	VerdictSummary string                  `json:"verdict_summary"`
	Comparisons    []ChannelComparisonItem `json:"comparisons"`
}

// ChannelComparisonItem is one winner-pattern comparison row.
type ChannelComparisonItem struct {
	Pattern     string `json:"pattern"`
	YourWinners string `json:"your_winners"`
	ThisScript  string `json:"this_script"`
	Fix         string `json:"fix"`
}

// VisualInsights is the thumbnail and frame analysis, present only when a
// thumbnail or a video was supplied.
type VisualInsights struct {
	ThumbnailScore     int             `json:"thumbnail_score"`
	ThumbnailFeedback  string          `json:"thumbnail_feedback"`
	EditingMoments     []EditingMoment `json:"editing_moments,omitempty"`
	VisualIssues       []string        `json:"visual_issues"`
	VisualImprovements []string        `json:"visual_improvements"`
	AttentionZones     []AttentionZone `json:"attention_zones,omitempty"`
	Heatmap            [][]float64     `json:"heatmap,omitempty"`
}

// EditingMoment is one editing observation.
type EditingMoment struct {
	FrameRef    string `json:"frame_ref"`
	Observation string `json:"observation"`
	Suggestion  string `json:"suggestion"`
	Impact      string `json:"impact"`
}

// AttentionZone is a region of the thumbnail heatmap. Coordinates are 0-1.
type AttentionZone struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Radius    float64 `json:"radius"`
	Intensity float64 `json:"intensity"`
	Type      string  `json:"type"` // face | text | object | contrast | focal_point
	Label     string  `json:"label"`
}

// Authenticity is the YouTube Partner Program "inauthentic or reused content"
// risk inside a report. Score is 0-100 where 100 is fully authentic.
type Authenticity struct {
	Score       int      `json:"score"`
	RiskLevel   string   `json:"risk_level"` // low | medium | high
	Issues      []string `json:"issues"`
	Suggestions []string `json:"suggestions"`
}

// HookAnalysis is the hook section of a report.
type HookAnalysis struct {
	Score            int      `json:"score"`
	HookDuration     int      `json:"hook_duration_seconds"`
	AttentionGrabber string   `json:"attention_grabber"`
	Strengths        []string `json:"strengths"`
	Weaknesses       []string `json:"weaknesses"`
	// Suggestions is stripped for non-entitled viewers.
	Suggestions []string `json:"suggestions"`
}

// StructureAnalysis is the structure section of a report.
type StructureAnalysis struct {
	Score       int              `json:"score"`
	Sections    []ContentSection `json:"sections"`
	FlowRating  string           `json:"flow_rating"`
	Strengths   []string         `json:"strengths"`
	Weaknesses  []string         `json:"weaknesses"`
	Suggestions []string         `json:"suggestions"`
}

// ContentSection is one section of the script's structure.
type ContentSection struct {
	Title           string `json:"title"`
	StartPercentage int    `json:"start_percentage"`
	EndPercentage   int    `json:"end_percentage"`
	Purpose         string `json:"purpose"`
	Quality         string `json:"quality"`
}

// PacingAnalysis is the pacing section of a report.
type PacingAnalysis struct {
	Score             int      `json:"score"`
	OverallPace       string   `json:"overall_pace"`
	EnergyVariation   string   `json:"energy_variation"`
	DropoffRiskPoints []int    `json:"dropoff_risk_points"`
	Strengths         []string `json:"strengths"`
	Weaknesses        []string `json:"weaknesses"`
	Suggestions       []string `json:"suggestions"`
}

// RetentionPoint is one point on the attention-risk curve. Timestamp is a
// percentage of the script, not a clock time.
type RetentionPoint struct {
	Timestamp int     `json:"timestamp_percentage"`
	Retention float64 `json:"retention"`
	Event     string  `json:"event,omitempty"`
	Sentiment string  `json:"sentiment,omitempty"`
}

// Improvement is one prioritized fix. A non-entitled viewer gets the title and
// the quote plus Locked=true; the rewrite fields arrive empty.
type Improvement struct {
	Priority        string `json:"priority,omitempty"`         // CRITICAL | HIGH | MEDIUM | LOW
	ProblemType     string `json:"problem_type,omitempty"`     // WEAK_HOOK | SLOW_START | …
	Timestamp       string `json:"timestamp,omitempty"`        // "0:00-0:18" or a position description
	RetentionImpact string `json:"retention_impact,omitempty"` // qualitative script-level risk
	OriginalQuote   string `json:"original_quote,omitempty"`
	Current         string `json:"current,omitempty"` // alias for OriginalQuote
	Improved        string `json:"improved,omitempty"`
	ProblemSummary  string `json:"problem_summary,omitempty"`
	Title           string `json:"title,omitempty"`
	Why             string `json:"why,omitempty"`
	WhyItWorks      string `json:"why_it_works,omitempty"`
	Category        string `json:"category,omitempty"`
	Description     string `json:"description,omitempty"`
	Impact          string `json:"impact,omitempty"`
	// Locked reports that the rewrite fields were stripped server-side.
	Locked bool `json:"locked,omitempty"`
}

// AnalysisPage is a page of the signed-in account's audits
// (dto.AnalysisFullListResponse). Items are gated the same way a single report
// is. Total is a heuristic: when the page came back full it is perPage*page+1.
type AnalysisPage struct {
	Analyses   []*Analysis `json:"analyses"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PerPage    int         `json:"per_page"`
	TotalPages int         `json:"total_pages"`
}

// HookResult is one hook analysis (dto.HookAnalyzeResponse), returned by both
// the create and the fetch routes.
type HookResult struct {
	EvaluationID       string         `json:"evaluation_id"`
	HookText           string         `json:"hook_text"`
	Niche              string         `json:"niche,omitempty"`
	OverallScore       int            `json:"overall_score"`
	Grade              string         `json:"grade"`
	OneSentenceVerdict string         `json:"one_sentence_verdict"`
	Sentences          []HookSentence `json:"sentences"`
	TopIssues          []HookIssue    `json:"top_issues"`
	Rewrites           []HookRewrite  `json:"rewrites"`
	ModelUsed          string         `json:"model_used"`
	PromptVersion      int            `json:"prompt_version"`
	DegradedFromPro    bool           `json:"degraded_from_pro,omitempty"`
	CreatedAt          time.Time      `json:"created_at"`
}

// HookSentence is one sentence of the hook with its attention numbers.
type HookSentence struct {
	Quote          string `json:"quote"`
	AttentionPull  int    `json:"attention_pull"`
	CuriosityGap   int    `json:"curiosity_gap"`
	PayoffDistance string `json:"payoff_distance"`
	Note           string `json:"note"`
}

// HookIssue is the headline problem with one sentence.
type HookIssue struct {
	Headline string `json:"headline"`
	Quote    string `json:"quote"`
	Why      string `json:"why"`
}

// HookRewrite is a rewritten hook in one of the offered styles.
type HookRewrite struct {
	Style    string `json:"style"`
	Hook     string `json:"hook"`
	WhyWorks string `json:"why_works"`
}

// AuthenticityResult is one standalone authenticity check
// (dto.AuthenticityCheckResponse).
type AuthenticityResult struct {
	AnalysisID   string                    `json:"analysis_id"`
	Score        int                       `json:"score"`
	RiskLevel    string                    `json:"risk_level"`
	Verdict      string                    `json:"verdict"`
	Signals      []AuthenticitySignal      `json:"signals"`
	Remediation  []AuthenticityRemediation `json:"remediation"`
	OverrideNote string                    `json:"override_note,omitempty"`
	// HeuristicReport is the deterministic layer's report, passed through so
	// the UI can quote its evidence. Its shape is the checker's own.
	HeuristicReport json.RawMessage `json:"heuristic_report"`
	CreatedAt       time.Time       `json:"created_at"`
}

// AuthenticitySignal is one per-signal breakdown.
type AuthenticitySignal struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Severity string `json:"severity"`
	Reason   string `json:"reason"`
	Quote    string `json:"quote,omitempty"`
}

// AuthenticityRemediation is one actionable fix.
type AuthenticityRemediation struct {
	Title   string `json:"title"`
	Current string `json:"current,omitempty"`
	Fix     string `json:"fix"`
	Why     string `json:"why"`
}

// PolicyResult is one standalone policy pre-flight
// (dto.PolicyCheckResponse).
type PolicyResult struct {
	PreflightID string          `json:"preflight_id"`
	Title       string          `json:"title,omitempty"`
	Preflight   PolicyPreflight `json:"preflight"`
	// Cached means this exact script and title had already been checked under
	// the same rubric and prompt version, and the stored result was replayed.
	Cached    bool      `json:"cached"`
	CreatedAt time.Time `json:"created_at"`
}

// PolicyPreflight is the shared policy result shape, used both as the whole
// response of the standalone checker and as a section of a full report.
type PolicyPreflight struct {
	// Verdict is "no_matches", "review_suggested" or "high_matches". It
	// describes how strongly the script matched the rubric, never what YouTube
	// will decide.
	Verdict string `json:"verdict"`
	// Flags carry severity and category always; the passage only when unlocked.
	Flags            []PolicyFlag   `json:"flags"`
	CountsByCategory map[string]int `json:"counts_by_category"`
	TotalFlags       int            `json:"total_flags"`
	// Locked reports that the passages were stripped server-side.
	Locked          bool `json:"locked,omitempty"`
	ScriptTruncated bool `json:"script_truncated,omitempty"`
	// RubricVersion and RubricPublishedAt stamp the snapshot of YouTube's
	// published guidelines this result was produced against.
	RubricVersion     int              `json:"rubric_version"`
	RubricPublishedAt time.Time        `json:"rubric_published_at"`
	Categories        []PolicyCategory `json:"categories"`
	// ScopeStatement is the honest-scope sentence that must accompany every
	// rendering of this result, sent from the server so no surface can drift.
	ScopeStatement string    `json:"scope_statement"`
	ModelUsed      string    `json:"model_used"`
	GeneratedAt    time.Time `json:"generated_at"`
}

// PolicyFlag is one matched passage. Quote, Why and SuggestedRewrite are absent
// on a gated response; Locked says why.
type PolicyFlag struct {
	CategoryID       string `json:"category_id"`
	Severity         string `json:"severity"`
	Quote            string `json:"quote,omitempty"`
	InTitle          bool   `json:"in_title,omitempty"`
	Why              string `json:"why,omitempty"`
	SuggestedRewrite string `json:"suggested_rewrite,omitempty"`
	Locked           bool   `json:"locked,omitempty"`
}

// PolicyCategory is one rubric entry, sent with every response so a client can
// render names, citations and the family consequence without keeping its own
// copy of the rubric.
type PolicyCategory struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	OfficialName string `json:"official_name"`
	// Family is "ad_suitability" or "community_guidelines". The two carry
	// different consequences and must be rendered distinctly.
	Family            string `json:"family"`
	FamilyLabel       string `json:"family_label"`
	FamilyConsequence string `json:"family_consequence"`
	SourceURL         string `json:"source_url"`
	SourceLabel       string `json:"source_label"`
	Summary           string `json:"summary"`
	// Detectability is "direct" or "partial". Partial means the category is
	// largely decided by footage and a script is only part of the picture;
	// DetectabilityNote carries the visible caveat for those.
	Detectability     string `json:"detectability"`
	DetectabilityNote string `json:"detectability_note,omitempty"`
}

// Subscription is GET /api/user/subscription (dto.GetSubscriptionResponse).
type Subscription struct {
	Plan      *SubscriptionPlan `json:"subscription,omitempty"`
	HasActive bool              `json:"has_active"`
	Message   string            `json:"message,omitempty"`
}

// SubscriptionPlan is the Polar subscription itself.
type SubscriptionPlan struct {
	ID                 string    `json:"id"`
	Status             string    `json:"status"`
	VariantID          string    `json:"variant_id"`
	CurrentPeriodStart time.Time `json:"current_period_start"`
	CurrentPeriodEnd   time.Time `json:"current_period_end"`
	CancelAtPeriodEnd  bool      `json:"cancel_at_period_end"`
	DaysRemaining      int       `json:"days_remaining"`
}

// UploadStatus is the shape every resumable upload call returns, so a client
// parses one thing whether it just created, resumed or appended.
type UploadStatus struct {
	UploadID string `json:"upload_id"`
	// Offset is how many bytes the server holds. After a 409 it is the offset
	// to resume from.
	Offset    int64 `json:"offset"`
	Size      int64 `json:"size"`
	Complete  bool  `json:"complete"`
	ChunkSize int64 `json:"chunk_size"`
}

// DeviceStart is the response of POST /api/cli/auth/start.
type DeviceStart struct {
	// DeviceCode is the secret the CLI polls with. It is never shown.
	DeviceCode string `json:"device_code"`
	// UserCode is the short code the user types or confirms in the browser.
	UserCode string `json:"user_code"`
	// VerificationURI is the approval page; VerificationURIComplete carries the
	// code as a query parameter so opening it needs no typing.
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	// ExpiresIn is the device code's lifetime in seconds, Interval the minimum
	// seconds between polls.
	ExpiresIn int `json:"expires_in"`
	Interval  int `json:"interval"`
}

// DeviceToken is the response of POST /api/cli/auth/token: the API key plus
// enough identity to render an account card before the first authenticated
// call. It is returned exactly once; the server consumes the device code.
type DeviceToken struct {
	APIKey    string `json:"api_key"`
	KeyID     string `json:"key_id"`
	KeyPrefix string `json:"key_prefix"`
	User      User   `json:"user"`
}
