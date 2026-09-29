package api

import (
	"encoding/json"
	"testing"
	"time"
)

// fullReportJSON is a complete, ungated report as the API serialises one:
// every section present, with the nested shapes and the field names the DTO
// uses. The tags here are the wire contract with the API, and a typo in one of
// them silently reads back as a zero value, so the assertions below check the
// values that only a correct tag can produce.
const fullReportJSON = `{
  "id": "6f2a4c1e-0000-4000-8000-000000000001",
  "video_title": "Why nobody finishes your videos",
  "script_text": "The first line of the script.",
  "video_duration": 615,
  "thumbnail_path": "thumbnails/a.png",
  "status": "completed",
  "progress": 100,
  "overall_score": 78,
  "hook_score": 82,
  "structure_score": 71,
  "pacing_score": 64,
  "hook_analysis": {
    "score": 82,
    "hook_duration_seconds": 12,
    "attention_grabber": "A direct promise",
    "strengths": ["opens with a claim"],
    "weaknesses": ["buries the payoff"],
    "suggestions": ["name the payoff sooner"]
  },
  "structure_analysis": {
    "score": 71,
    "sections": [{"title": "Cold open", "start_percentage": 0, "end_percentage": 12, "purpose": "hook", "quality": "strong"}],
    "flow_rating": "good",
    "strengths": ["clear promise"],
    "weaknesses": ["sagging middle"],
    "suggestions": ["cut the recap"]
  },
  "pacing_analysis": {
    "score": 64,
    "overall_pace": "uneven",
    "energy_variation": "low",
    "dropoff_risk_points": [15, 52],
    "strengths": ["fast first minute"],
    "weaknesses": ["flat second act"],
    "suggestions": ["add a turn at 4:00"]
  },
  "retention_curve": [
    {"timestamp_percentage": 0, "retention": 100, "event": "hook", "sentiment": "positive"},
    {"timestamp_percentage": 45, "retention": 61}
  ],
  "retention_curve_source": "model",
  "previous_analysis_id": "11111111-1111-4111-8111-111111111111",
  "revision_summary": {
    "draft_number": 2,
    "first_analysis_id": "22222222-2222-4222-8222-222222222222",
    "first_score": 61,
    "previous_score": 70,
    "implemented": [{"title": "Cut the intro", "priority": "HIGH", "draft": 2}],
    "resolved_count": 1
  },
  "ready_to_record": true,
  "policy_preflight": {
    "verdict": "review_suggested",
    "flags": [{"category_id": "violence", "severity": "medium", "quote": "shoot them down", "in_title": false, "why": "combat framing", "suggested_rewrite": "beat them", "locked": false}],
    "counts_by_category": {"violence": 1},
    "total_flags": 1,
    "script_truncated": false,
    "rubric_version": 3,
    "rubric_published_at": "2026-01-05T00:00:00Z",
    "categories": [{"id": "violence", "label": "Violence", "official_name": "Violent content", "family": "ad_suitability", "family_label": "Ad suitability", "family_consequence": "limited ads", "source_url": "https://example.com", "source_label": "YouTube", "summary": "…", "detectability": "partial", "detectability_note": "Your script is only part of it."}],
    "scope_statement": "This is not a prediction of YouTube's decision.",
    "model_used": "gpt-x",
    "generated_at": "2026-02-03T04:05:00Z"
  },
  "improvements": [{
    "priority": "CRITICAL",
    "problem_type": "WEAK_HOOK",
    "timestamp": "0:00-0:18",
    "retention_impact": "high",
    "original_quote": "Hey guys, welcome back.",
    "current": "Hey guys, welcome back.",
    "improved": "You are editing your video wrong.",
    "problem_summary": "The first line asks for attention instead of earning it.",
    "title": "Rewrite the first line",
    "why": "A claim beats a greeting",
    "why_it_works": "It promises a payoff",
    "category": "hook",
    "description": "legacy copy",
    "impact": "high"
  }],
  "one_key_improvement": "Rewrite the first line.",
  "visual_insights": {
    "thumbnail_score": 55,
    "thumbnail_feedback": "The face is too small.",
    "editing_moments": [{"frame_ref": "0:12", "observation": "static shot", "suggestion": "cut to the chart", "impact": "high"}],
    "visual_issues": ["low contrast"],
    "visual_improvements": ["crop tighter"],
    "attention_zones": [{"x": 0.5, "y": 0.4, "radius": 0.2, "intensity": 0.9, "type": "face", "label": "presenter"}],
    "heatmap": [[0.1, 0.2], [0.3, 0.4]]
  },
  "authenticity": {"score": 88, "risk_level": "low", "issues": [], "suggestions": ["keep the original b-roll"]},
  "score_explanations": {"hook": "the promise is clear", "structure": "the middle sags", "pacing": "the second act is flat"},
  "title_rewrite": {"original": "My video", "improved": "Why nobody finishes your videos", "why": "it names the problem"},
  "channel_comparison": {"verdict_summary": "You open slower than your winners.", "comparisons": [{"pattern": "cold open", "your_winners": "start mid-action", "this_script": "starts with a greeting", "fix": "cut the greeting"}]},
  "xray_evidence": {"status": "ready", "passages": [{"index": 0, "level": "high"}]},
  "processing_time_ms": 42000,
  "created_at": "2026-02-03T04:05:06Z",
  "locked": false,
  "tier": "paid"
}`

func TestAnalysisDecodesEverySection(t *testing.T) {
	var a Analysis
	if err := json.Unmarshal([]byte(fullReportJSON), &a); err != nil {
		t.Fatalf("unmarshal full report: %v", err)
	}

	if a.ID != "6f2a4c1e-0000-4000-8000-000000000001" || a.Status != StatusCompleted || a.Progress != 100 {
		t.Errorf("header = %+v", a)
	}
	if a.VideoDuration == nil || *a.VideoDuration != 615 {
		t.Errorf("VideoDuration = %v, want 615", a.VideoDuration)
	}
	if a.HookScore == nil || *a.HookScore != 82 || *a.StructureScore != 71 || *a.PacingScore != 64 {
		t.Errorf("component scores = %v/%v/%v", a.HookScore, a.StructureScore, a.PacingScore)
	}
	if !a.CreatedAt.Equal(time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)) {
		t.Errorf("CreatedAt = %v", a.CreatedAt)
	}

	// The curve's x axis is a percentage, and the tag is timestamp_percentage.
	// A wrong tag would leave Timestamp at 0 for every point.
	if len(a.RetentionCurve) != 2 {
		t.Fatalf("RetentionCurve = %+v", a.RetentionCurve)
	}
	if a.RetentionCurve[1].Timestamp != 45 || a.RetentionCurve[1].Retention != 61 {
		t.Errorf("curve point = %+v, want timestamp 45 and retention 61", a.RetentionCurve[1])
	}
	if a.RetentionCurve[0].Event != "hook" || a.RetentionCurve[0].Sentiment != "positive" {
		t.Errorf("curve point event/sentiment = %+v", a.RetentionCurve[0])
	}
	if a.RetentionCurveSource != "model" {
		t.Errorf("RetentionCurveSource = %q", a.RetentionCurveSource)
	}

	if a.HookAnalysis == nil || a.HookAnalysis.HookDuration != 12 || a.HookAnalysis.AttentionGrabber != "A direct promise" {
		t.Errorf("HookAnalysis = %+v", a.HookAnalysis)
	}
	if a.StructureAnalysis == nil || len(a.StructureAnalysis.Sections) != 1 ||
		a.StructureAnalysis.Sections[0].StartPercentage != 0 || a.StructureAnalysis.Sections[0].EndPercentage != 12 {
		t.Errorf("StructureAnalysis = %+v", a.StructureAnalysis)
	}
	if a.PacingAnalysis == nil || a.PacingAnalysis.DropoffRiskPoints[1] != 52 || a.PacingAnalysis.OverallPace != "uneven" {
		t.Errorf("PacingAnalysis = %+v", a.PacingAnalysis)
	}

	if a.RevisionSummary == nil || a.RevisionSummary.DraftNumber != 2 || a.RevisionSummary.ResolvedCount != 1 {
		t.Errorf("RevisionSummary = %+v", a.RevisionSummary)
	}
	if a.PreviousAnalysisID != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("PreviousAnalysisID = %q", a.PreviousAnalysisID)
	}
	if !a.ReadyToRecord {
		t.Error("ReadyToRecord is false; the tag is ready_to_record")
	}

	if len(a.Improvements) != 1 {
		t.Fatalf("Improvements = %+v", a.Improvements)
	}
	imp := a.Improvements[0]
	if imp.Priority != "CRITICAL" || imp.Timestamp != "0:00-0:18" || imp.Improved == "" || imp.WhyItWorks == "" {
		t.Errorf("Improvement = %+v", imp)
	}
	if imp.Locked {
		t.Error("a paid report's improvement must not be marked locked")
	}

	if a.PolicyPreflight == nil {
		t.Fatal("PolicyPreflight is nil")
	}
	if a.PolicyPreflight.Verdict != "review_suggested" || a.PolicyPreflight.CountsByCategory["violence"] != 1 ||
		a.PolicyPreflight.TotalFlags != 1 || a.PolicyPreflight.RubricVersion != 3 {
		t.Errorf("PolicyPreflight = %+v", a.PolicyPreflight)
	}
	if len(a.PolicyPreflight.Flags) != 1 || a.PolicyPreflight.Flags[0].SuggestedRewrite != "beat them" {
		t.Errorf("policy flags = %+v", a.PolicyPreflight.Flags)
	}
	if len(a.PolicyPreflight.Categories) != 1 || a.PolicyPreflight.Categories[0].DetectabilityNote == "" ||
		a.PolicyPreflight.Categories[0].Family != "ad_suitability" {
		t.Errorf("policy categories = %+v", a.PolicyPreflight.Categories)
	}

	if a.VisualInsights == nil || a.VisualInsights.ThumbnailScore != 55 {
		t.Fatalf("VisualInsights = %+v", a.VisualInsights)
	}
	if got := a.VisualInsights.Heatmap; len(got) != 2 || len(got[0]) != 2 || got[1][0] != 0.3 {
		t.Errorf("Heatmap = %v", a.VisualInsights.Heatmap)
	}
	if len(a.VisualInsights.AttentionZones) != 1 || a.VisualInsights.AttentionZones[0].Type != "face" {
		t.Errorf("AttentionZones = %+v", a.VisualInsights.AttentionZones)
	}

	if a.Authenticity == nil || a.Authenticity.Score != 88 || a.Authenticity.RiskLevel != "low" {
		t.Errorf("Authenticity = %+v", a.Authenticity)
	}
	if a.ScoreExplanations == nil || a.ScoreExplanations.Pacing == "" {
		t.Errorf("ScoreExplanations = %+v", a.ScoreExplanations)
	}
	if a.TitleRewrite == nil || a.TitleRewrite.Improved != "Why nobody finishes your videos" {
		t.Errorf("TitleRewrite = %+v", a.TitleRewrite)
	}
	if a.ChannelComparison == nil || len(a.ChannelComparison.Comparisons) != 1 {
		t.Errorf("ChannelComparison = %+v", a.ChannelComparison)
	}
	if a.ProcessingTimeMs == nil || *a.ProcessingTimeMs != 42000 {
		t.Errorf("ProcessingTimeMs = %v", a.ProcessingTimeMs)
	}
	if string(a.XrayEvidence) == "" {
		t.Error("XrayEvidence was dropped; it is passed through raw on purpose")
	}
	if a.Tier != TierPaid {
		t.Errorf("Tier = %q, want paid", a.Tier)
	}
}

func TestAnalysisDecodesAGatedPendingReport(t *testing.T) {
	// What POST /api/analyze answers with: a pending job and almost nothing
	// else. None of the optional sections are present, and the required slices
	// arrive null.
	body := `{"id":"a-1","video_title":"Draft","status":"pending","progress":0,"overall_score":0,"retention_curve":[],"improvements":[],"one_key_improvement":"","ready_to_record":false,"created_at":"2026-02-03T04:05:06Z","locked":true,"lock_reason":"signup_required","tier":"anonymous"}`

	var a Analysis
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("unmarshal pending report: %v", err)
	}
	if a.Status != StatusPending || a.Terminal() {
		t.Errorf("status = %q, terminal = %t, want a non-terminal pending job", a.Status, a.Terminal())
	}
	if a.HookAnalysis != nil || a.TitleRewrite != nil || a.PolicyPreflight != nil || a.VideoDuration != nil {
		t.Errorf("absent sections decoded as present: %+v", a)
	}
	if !a.Locked || a.LockReason != LockReasonSignupRequired {
		t.Errorf("locked = %t, reason = %q", a.Locked, a.LockReason)
	}
	if a.ScriptText != "" || a.ThumbnailPath != "" {
		t.Errorf("ScriptText/ThumbnailPath = %q/%q, want empty", a.ScriptText, a.ThumbnailPath)
	}
}

func TestDeviceAndUploadShapes(t *testing.T) {
	var start DeviceStart
	if err := json.Unmarshal([]byte(`{"device_code":"dc","user_code":"BCDF-GHJK","verification_uri":"https://prepublish.ai/cli/login","verification_uri_complete":"https://prepublish.ai/cli/login?code=BCDF-GHJK","expires_in":600,"interval":2}`), &start); err != nil {
		t.Fatalf("unmarshal DeviceStart: %v", err)
	}
	if start.DeviceCode != "dc" || start.UserCode != "BCDF-GHJK" || start.ExpiresIn != 600 || start.Interval != 2 {
		t.Errorf("DeviceStart = %+v", start)
	}
	if start.VerificationURIComplete == "" {
		t.Error("VerificationURIComplete was dropped; it is the link that needs no typing")
	}

	var token DeviceToken
	if err := json.Unmarshal([]byte(`{"api_key":"pp_live_x","key_id":"k-1","key_prefix":"pp_live_x","user":{"id":"u-1","email":"a@b.c","subscription_status":"active","subscription_id":"sub_1","analysis_count":2,"created_at":"2026-01-01T00:00:00Z"}}`), &token); err != nil {
		t.Fatalf("unmarshal DeviceToken: %v", err)
	}
	if token.APIKey != "pp_live_x" || token.User.SubscriptionID != "sub_1" || token.User.SubscriptionStatus != "active" {
		t.Errorf("DeviceToken = %+v", token)
	}

	// The upload status shape is shared by create, status and append.
	var st UploadStatus
	if err := json.Unmarshal([]byte(`{"upload_id":"u-1","offset":8388608,"size":52428800,"complete":false,"chunk_size":8388608}`), &st); err != nil {
		t.Fatalf("unmarshal UploadStatus: %v", err)
	}
	if st.Offset != 8388608 || st.Size != 52428800 || st.Complete || st.ChunkSize != 8388608 {
		t.Errorf("UploadStatus = %+v", st)
	}

	var page AnalysisPage
	if err := json.Unmarshal([]byte(`{"analyses":[{"id":"a-1","video_title":"t","status":"completed"}],"total":11,"page":2,"per_page":10,"total_pages":2}`), &page); err != nil {
		t.Fatalf("unmarshal AnalysisPage: %v", err)
	}
	if len(page.Analyses) != 1 || page.Analyses[0].ID != "a-1" || page.Total != 11 || page.TotalPages != 2 {
		t.Errorf("AnalysisPage = %+v", page)
	}
}
