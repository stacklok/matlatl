package reference

import "testing"

func TestLinkType_StringValid(t *testing.T) {
	all := []LinkType{
		RelativeLink, Wikilink, Anchor, ImageEmbed, Transclusion,
		FrontmatterRelated, External,
	}
	seen := make(map[string]bool)
	for _, lt := range all {
		if !lt.Valid() {
			t.Errorf("LinkType %d reported invalid", int(lt))
		}
		s := lt.String()
		if s == "" || s == "unknown" {
			t.Errorf("LinkType %d has bad String() %q", int(lt), s)
		}
		if seen[s] {
			t.Errorf("duplicate String() %q", s)
		}
		seen[s] = true
	}
	if (LinkType(-1)).Valid() || (LinkType(99)).Valid() {
		t.Error("out-of-range LinkType reported valid")
	}
	if got := LinkType(99).String(); got != "unknown" {
		t.Errorf("LinkType(99).String() = %q, want unknown", got)
	}
}

func TestLinkHealth_StringValid(t *testing.T) {
	all := []LinkHealth{
		Unresolved, Valid, Broken, BrokenAnchor, NonNote, Ambiguous,
		ExternalHealth, Ignored,
	}
	if Unresolved != 0 {
		t.Error("Unresolved must be the zero value")
	}
	for _, h := range all {
		if !h.Valid() {
			t.Errorf("LinkHealth %d reported invalid", int(h))
		}
		if s := h.String(); s == "" || s == "unknown" {
			t.Errorf("LinkHealth %d has bad String() %q", int(h), s)
		}
	}
	if (LinkHealth(99)).Valid() {
		t.Error("out-of-range LinkHealth reported valid")
	}
}

func TestTargetKind_StringValid(t *testing.T) {
	all := []TargetKind{TargetNone, TargetDocument, TargetSection, TargetAsset, TargetExternal}
	if TargetNone != 0 {
		t.Error("TargetNone must be the zero value")
	}
	for _, k := range all {
		if !k.Valid() {
			t.Errorf("TargetKind %d reported invalid", int(k))
		}
		if s := k.String(); s == "" || s == "unknown" {
			t.Errorf("TargetKind %d has bad String() %q", int(k), s)
		}
	}
	if (TargetKind(99)).Valid() {
		t.Error("out-of-range TargetKind reported valid")
	}
}

func TestResolutionPolicy_StringValid(t *testing.T) {
	all := []ResolutionPolicy{Exact, LongestSuffix, Basename}
	for _, p := range all {
		if !p.Valid() {
			t.Errorf("ResolutionPolicy %d reported invalid", int(p))
		}
		if s := p.String(); s == "" || s == "unknown" {
			t.Errorf("ResolutionPolicy %d has bad String() %q", int(p), s)
		}
	}
	if DefaultResolutionPolicy != LongestSuffix {
		t.Errorf("DefaultResolutionPolicy = %v, want LongestSuffix", DefaultResolutionPolicy)
	}
	if (ResolutionPolicy(99)).Valid() {
		t.Error("out-of-range ResolutionPolicy reported valid")
	}
}
