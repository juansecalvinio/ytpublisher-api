# Session 6: YouTube Rule Validation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Validate generated content against YouTube's real constraints — title ≤100 characters, tags ≤500 characters combined, description hook ≤125 characters and positioned at the start — as pure, dependency-free functions.

**Architecture:** `internal/rules` has no dependencies on storage, HTTP, or any external service — it's pure functions over plain data, matching `internal/styleanalysis`'s design. Each rule returns a list of `Violation`s (not just true/false) with a field name and message, because the future generation pipeline (Session 7) needs to know *which* field failed and *why* to ask the LLM for a targeted repair — a bare boolean wouldn't carry enough information for that.

**Tech Stack:** No new dependencies. Standard library only.

**Spec:** `docs/superpowers/specs/2026-08-24-ytpublisher-api-v1-design.md`

## Global Constraints

- Go version: 1.22+
- Module path: `github.com/juansecalvinio/ytpublisher-api`
- Rule limits (from the spec): title ≤100 characters, tags ≤500 characters combined, description hook ≤125 characters
- `internal/rules` is not wired into `cmd/api` or any HTTP endpoint in this session — it has no consumer yet. Session 7 (LLM generation) is what calls it, as part of the generate → validate → repair loop described in the spec.

---

### Task 1: `ValidateTitle`

**Files:**
- Create: `internal/rules/title.go`
- Test: `internal/rules/title_test.go`

**Interfaces:**
- Produces: `rules.Violation{Field, Message string}`, `rules.MaxTitleLength` (const, 100), `rules.ValidateTitle(title string) []Violation`

- [ ] **Step 1: Write the failing tests**

```go
// internal/rules/title_test.go
package rules

import (
	"strings"
	"testing"
)

func TestValidateTitle_AllowsExactly100Characters(t *testing.T) {
	title := strings.Repeat("a", 100)

	violations := ValidateTitle(title)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for a 100-character title", violations)
	}
}

func TestValidateTitle_RejectsExactly101Characters(t *testing.T) {
	title := strings.Repeat("a", 101)

	violations := ValidateTitle(title)

	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 for a 101-character title", len(violations))
	}
	if violations[0].Field != "title" {
		t.Errorf("violations[0].Field = %q, want %q", violations[0].Field, "title")
	}
}

func TestValidateTitle_AllowsEmptyTitle(t *testing.T) {
	violations := ValidateTitle("")

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for an empty title (length-only rule)", violations)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/...`
Expected: FAIL — compile error, `ValidateTitle` undefined.

- [ ] **Step 3: Write `title.go`**

```go
// internal/rules/title.go
package rules

import "fmt"

const MaxTitleLength = 100

type Violation struct {
	Field   string
	Message string
}

func ValidateTitle(title string) []Violation {
	if len(title) > MaxTitleLength {
		return []Violation{{
			Field:   "title",
			Message: fmt.Sprintf("title is %d characters, must be %d or fewer", len(title), MaxTitleLength),
		}}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v`
Expected: all 3 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/rules
git commit -m "feat: add title length rule validation"
```

---

### Task 2: `ValidateTags`

**Files:**
- Create: `internal/rules/tags.go`
- Test: `internal/rules/tags_test.go`

**Interfaces:**
- Consumes: `rules.Violation` (Task 1)
- Produces: `rules.MaxTagsTotalLength` (const, 500), `rules.ValidateTags(tags []string) []Violation`

- [ ] **Step 1: Write the failing tests**

```go
// internal/rules/tags_test.go
package rules

import (
	"strings"
	"testing"
)

func TestValidateTags_AllowsExactly500TotalCharacters(t *testing.T) {
	tags := []string{strings.Repeat("a", 250), strings.Repeat("b", 250)}

	violations := ValidateTags(tags)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for exactly 500 total characters", violations)
	}
}

func TestValidateTags_RejectsOver500TotalCharacters(t *testing.T) {
	tags := []string{strings.Repeat("a", 250), strings.Repeat("b", 251)}

	violations := ValidateTags(tags)

	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 for 501 total characters", len(violations))
	}
	if violations[0].Field != "tags" {
		t.Errorf("violations[0].Field = %q, want %q", violations[0].Field, "tags")
	}
}

func TestValidateTags_AllowsEmptyTagsList(t *testing.T) {
	violations := ValidateTags(nil)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for an empty tags list", violations)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -run TestValidateTags`
Expected: FAIL — compile error, `ValidateTags` undefined.

- [ ] **Step 3: Write `tags.go`**

```go
// internal/rules/tags.go
package rules

import "fmt"

const MaxTagsTotalLength = 500

func ValidateTags(tags []string) []Violation {
	total := 0
	for _, tag := range tags {
		total += len(tag)
	}
	if total > MaxTagsTotalLength {
		return []Violation{{
			Field:   "tags",
			Message: fmt.Sprintf("tags total %d characters, must be %d or fewer", total, MaxTagsTotalLength),
		}}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v`
Expected: all 6 tests PASS (Task 1's 3 plus Task 2's 3)

- [ ] **Step 5: Commit**

```bash
git add internal/rules
git commit -m "feat: add tags total length rule validation"
```

---

### Task 3: `ValidateDescriptionHook` and the combined `Validate`

**Files:**
- Create: `internal/rules/description.go`
- Test: `internal/rules/description_test.go`
- Create: `internal/rules/validate.go`
- Test: `internal/rules/validate_test.go`

**Interfaces:**
- Consumes: `rules.Violation`, `ValidateTitle`, `ValidateTags` (Tasks 1-2)
- Produces: `rules.MaxHookLength` (const, 125), `rules.ValidateDescriptionHook(description, hook string) []Violation`, `rules.GeneratedContent{Title, Description, Hook string; Tags []string}`, `rules.Validate(content GeneratedContent) []Violation`

- [ ] **Step 1: Write the failing description hook tests**

```go
// internal/rules/description_test.go
package rules

import (
	"strings"
	"testing"
)

func TestValidateDescriptionHook_AllowsHookAtExactly125Characters(t *testing.T) {
	hook := strings.Repeat("a", 125)
	description := hook + " rest of the description"

	violations := ValidateDescriptionHook(description, hook)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for a 125-character hook", violations)
	}
}

func TestValidateDescriptionHook_RejectsHookOver125Characters(t *testing.T) {
	hook := strings.Repeat("a", 126)
	description := hook + " rest of the description"

	violations := ValidateDescriptionHook(description, hook)

	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 for a 126-character hook", len(violations))
	}
	if violations[0].Field != "description_hook" {
		t.Errorf("violations[0].Field = %q, want %q", violations[0].Field, "description_hook")
	}
}

func TestValidateDescriptionHook_RequiresDescriptionToStartWithHook(t *testing.T) {
	violations := ValidateDescriptionHook("Something else entirely", "Expected hook")

	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 when description does not start with the hook", len(violations))
	}
	if violations[0].Field != "description_hook" {
		t.Errorf("violations[0].Field = %q, want %q", violations[0].Field, "description_hook")
	}
}

func TestValidateDescriptionHook_ReturnsBothViolationsWhenHookIsTooLongAndMisplaced(t *testing.T) {
	hook := strings.Repeat("a", 126)

	violations := ValidateDescriptionHook("does not start with the hook", hook)

	if len(violations) != 2 {
		t.Fatalf("len(violations) = %d, want 2 (too long AND misplaced)", len(violations))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -run TestValidateDescriptionHook`
Expected: FAIL — compile error, `ValidateDescriptionHook` undefined.

- [ ] **Step 3: Write `description.go`**

```go
// internal/rules/description.go
package rules

import (
	"fmt"
	"strings"
)

const MaxHookLength = 125

func ValidateDescriptionHook(description, hook string) []Violation {
	var violations []Violation

	if len(hook) > MaxHookLength {
		violations = append(violations, Violation{
			Field:   "description_hook",
			Message: fmt.Sprintf("hook is %d characters, must be %d or fewer", len(hook), MaxHookLength),
		})
	}

	if !strings.HasPrefix(description, hook) {
		violations = append(violations, Violation{
			Field:   "description_hook",
			Message: "description must start with the hook",
		})
	}

	return violations
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -run TestValidateDescriptionHook -v`
Expected: all 4 tests PASS

- [ ] **Step 5: Write the failing combined-validation tests**

```go
// internal/rules/validate_test.go
package rules

import (
	"strings"
	"testing"
)

func TestValidate_ReturnsNoViolationsForValidContent(t *testing.T) {
	content := GeneratedContent{
		Title:       "A perfectly reasonable title",
		Hook:        "A short, punchy hook.",
		Description: "A short, punchy hook. And then the rest of the description follows here.",
		Tags:        []string{"go", "programming"},
	}

	violations := Validate(content)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for fully valid content", violations)
	}
}

func TestValidate_AggregatesViolationsFromAllRules(t *testing.T) {
	content := GeneratedContent{
		Title:       strings.Repeat("a", 101),
		Hook:        strings.Repeat("b", 126),
		Description: strings.Repeat("b", 126) + " rest",
		Tags:        []string{strings.Repeat("c", 501)},
	}

	violations := Validate(content)

	fields := map[string]bool{}
	for _, v := range violations {
		fields[v.Field] = true
	}
	for _, want := range []string{"title", "tags", "description_hook"} {
		if !fields[want] {
			t.Errorf("expected a violation for field %q, got violations: %v", want, violations)
		}
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/rules/... -run TestValidate_`
Expected: FAIL — compile error, `GeneratedContent` and `Validate` undefined.

- [ ] **Step 7: Write `validate.go`**

```go
// internal/rules/validate.go
package rules

type GeneratedContent struct {
	Title       string
	Description string
	Hook        string
	Tags        []string
}

func Validate(content GeneratedContent) []Violation {
	var violations []Violation
	violations = append(violations, ValidateTitle(content.Title)...)
	violations = append(violations, ValidateTags(content.Tags)...)
	violations = append(violations, ValidateDescriptionHook(content.Description, content.Hook)...)
	return violations
}
```

- [ ] **Step 8: Run the full package test suite**

Run: `go test ./internal/rules/... -v`
Expected: all 12 tests PASS

- [ ] **Step 9: Commit**

```bash
git add internal/rules
git commit -m "feat: add description hook rule and combined content validation"
```

---

## Session 6 done when

- `go test ./internal/rules/...` passes with all boundary cases covered (100 vs 101 chars, 500 vs 501 chars, 125 vs 126 chars, hook-not-at-start)
- `Validate(content)` returns one `Violation` per broken rule, each naming the specific field, so Session 7's repair loop can ask the LLM to fix exactly what's wrong
