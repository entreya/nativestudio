package agent

import (
	"context"
	"testing"
)

func TestFactSubjectKeyIsWordOrderIndependent(t *testing.T) {
	if factSubjectKey("what is the latest PHP release") != factSubjectKey("latest release of PHP?") {
		t.Fatal("word order and filler words must not change the subject key")
	}
}

// TestRecallSurvivesRewording is the property that actually matters: a
// settled question asked again in different words must still hit the stored
// fact. Keys alone are too brittle for that (an unusual phrasing keys
// differently), so recall falls back to token overlap — this test pins the
// end-to-end behaviour rather than the internal key format.
func TestRecallSurvivesRewording(t *testing.T) {
	agentInstance, _, _ := newAgentTestEnvironment(t)
	ctx := context.Background()

	agentInstance.rememberVerifiedFact(ctx, "project", "what is the latest PHP release",
		"PHP 8.5 is the latest release.", []string{"phoronix.com"}, "cross_source")

	for _, rewording := range []string{
		"latest release of PHP?",
		"PHP release, the latest one",
		"tell me the newest PHP release please",
	} {
		if _, ok := agentInstance.recallVerifiedFact(ctx, "project", rewording); !ok {
			t.Errorf("failed to recall the verified fact for %q", rewording)
		}
	}

	// A genuinely different subject must not be answered from this fact.
	if _, ok := agentInstance.recallVerifiedFact(ctx, "project", "what is the latest Python release"); ok {
		t.Error("a different subject must not match a stored PHP fact")
	}
}

func TestFactSubjectKeyDistinguishesDifferentSubjects(t *testing.T) {
	if factSubjectKey("latest PHP release") == factSubjectKey("latest Python release") {
		t.Fatal("different subjects must not collide onto one key")
	}
}

func TestUserConfirmedVerificationDetectsTheFollowUpAnswer(t *testing.T) {
	// The frontend wraps a chosen option in this envelope before sending it
	// back (ChatPanel.jsx submitFollowUp).
	confirmed := `Regarding your question "I could only find weak corroboration for this...", my answer is: That's correct — save it as verified`
	if !userConfirmedVerification(confirmed) {
		t.Fatal("expected the confirmation option to be recognised")
	}
	for _, other := range []string{
		`Regarding your question "...", my answer is: Search again with better terms`,
		`Regarding your question "...", my answer is: Drop it, I'll find out myself`,
		"rename SiteController to KlopController",
	} {
		if userConfirmedVerification(other) {
			t.Errorf("did not expect %q to count as a confirmation", other)
		}
	}
}

func TestVerifiedFactRoundTrip(t *testing.T) {
	agentInstance, _, _ := newAgentTestEnvironment(t)
	ctx := context.Background()

	if _, ok := agentInstance.recallVerifiedFact(ctx, "project", "what is the latest PHP release"); ok {
		t.Fatal("expected no fact before anything was verified")
	}

	agentInstance.rememberVerifiedFact(ctx, "project", "what is the latest PHP release",
		"PHP 8.5 is the latest release.", []string{"phoronix.com", "9to5linux.com"}, "cross_source")

	// Recall must work for a differently-worded version of the same question.
	note, ok := agentInstance.recallVerifiedFact(ctx, "project", "latest release of PHP?")
	if !ok {
		t.Fatal("expected the verified fact to be recalled for an equivalent question")
	}
	for _, expected := range []string{"PHP 8.5", "phoronix.com", "PREVIOUSLY VERIFIED"} {
		if !contains(note, expected) {
			t.Errorf("recalled note missing %q:\n%s", expected, note)
		}
	}
}

func TestRememberVerifiedFactRejectsEmptyContent(t *testing.T) {
	agentInstance, _, _ := newAgentTestEnvironment(t)
	ctx := context.Background()

	agentInstance.rememberVerifiedFact(ctx, "project", "some question", "   ", nil, "cross_source")
	if _, ok := agentInstance.recallVerifiedFact(ctx, "project", "some question"); ok {
		t.Fatal("an empty fact must not be stored")
	}
}

func TestPromoteLastAnswerUsesTheQuestionBeforeTheConfirmation(t *testing.T) {
	agentInstance, _, _ := newAgentTestEnvironment(t)
	ctx := context.Background()

	history := []ctxMessage{
		{Role: "user", Content: "what is the latest PHP release"},
		{Role: "assistant", Content: "PHP 8.5 is the latest release."},
		{Role: "user", Content: `Regarding your question "...", my answer is: That's correct — save it as verified`},
	}
	agentInstance.promoteLastAnswerToVerifiedFact(ctx, "project", history)

	note, ok := agentInstance.recallVerifiedFact(ctx, "project", "latest PHP release")
	if !ok {
		t.Fatal("expected the confirmed claim to be stored under the original question's subject")
	}
	if !contains(note, "PHP 8.5") {
		t.Fatalf("stored the wrong claim:\n%s", note)
	}
	if !contains(note, "confirmed directly by the user") {
		t.Fatalf("expected the note to record that a human verified it:\n%s", note)
	}
}

// TestRecallApprovedChangesIgnoresUnapprovedRows is the safety property for
// procedural recall: session_learnings holds both user-approved outcomes and
// rows the agent wrote about its own runs (status ''), where the "result" is
// just the model's own claim. Recalling the latter would feed its own
// unverified assertions back to it as precedent.
func TestRecallApprovedChangesIgnoresUnapprovedRows(t *testing.T) {
	agentInstance, _, sessionID := newAgentTestEnvironment(t)
	ctx := context.Background()

	// An unverified row, exactly as agent.Run records one.
	if err := agentInstance.DB.RecordSessionLearning(ctx, "project", sessionID, "run-1",
		`{"change_summary":"renamed SiteController and claimed success","files_changed":["controllers/SiteController.php"]}`, ""); err != nil {
		t.Fatal(err)
	}
	if note := agentInstance.recallApprovedChanges(ctx, "project", "rename SiteController"); note != "" {
		t.Fatalf("unapproved work must not be recalled as precedent:\n%s", note)
	}

	// The same shape of row, but user-approved.
	if err := agentInstance.DB.RecordSessionLearning(ctx, "project", sessionID, "change:1",
		`{"change_summary":"renamed SiteController to KlopController","files_changed":["controllers/SiteController.php"]}`, "approved"); err != nil {
		t.Fatal(err)
	}
	note := agentInstance.recallApprovedChanges(ctx, "project", "rename SiteController again")
	if note == "" {
		t.Fatal("expected approved work to be recalled")
	}
	if !contains(note, "KlopController") {
		t.Fatalf("recalled note missing the approved change:\n%s", note)
	}
}

func TestRecallApprovedChangesFiltersByRelevance(t *testing.T) {
	agentInstance, _, sessionID := newAgentTestEnvironment(t)
	ctx := context.Background()

	if err := agentInstance.DB.RecordSessionLearning(ctx, "project", sessionID, "change:1",
		`{"change_summary":"updated the login view styling","files_changed":["views/site/login.php"]}`, "approved"); err != nil {
		t.Fatal(err)
	}
	if note := agentInstance.recallApprovedChanges(ctx, "project", "add a database migration for orders"); note != "" {
		t.Fatalf("unrelated past work must not be injected:\n%s", note)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
