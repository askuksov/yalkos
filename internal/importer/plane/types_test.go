package plane

import (
	"encoding/json"
	"testing"
)

func TestAttachmentDecode(t *testing.T) {
	cases := map[string]struct {
		in       string
		name     string
		size     int64
		uploaded bool
	}{
		"plane 1.3.1 shape": {
			`{"id": "a1", "size": 173254.0, "is_uploaded": true,
			  "attributes": {"name": "shot.png", "size": 173254, "type": "image/png"}}`,
			"shot.png", 173254, true,
		},
		"float attributes size": {`{"attributes": {"name": "a.txt", "size": 12.0}}`, "a.txt", 12, true},
		"attributes size wins":  {`{"size": 99.0, "attributes": {"name": "a.txt", "size": 12}}`, "a.txt", 12, true},
		"top-level fallback":    {`{"size": 99.0, "attributes": {"name": "a.txt"}}`, "a.txt", 99, true},
		"fraction rounds up":    {`{"size": 10.2, "attributes": {"name": "a.txt"}}`, "a.txt", 11, true},
		"negative is zero":      {`{"size": -5, "attributes": {"name": "a.txt", "size": -1}}`, "a.txt", 0, true},
		"no sizes":              {`{"attributes": {"name": "a.txt"}}`, "a.txt", 0, true},
		"null sizes":            {`{"size": null, "attributes": {"name": "a.txt", "size": null}}`, "a.txt", 0, true},
		"not uploaded":          {`{"is_uploaded": false, "attributes": {"name": "a.txt", "size": 1}}`, "a.txt", 1, false},
		"uploaded null":         {`{"is_uploaded": null, "attributes": {"name": "a.txt"}}`, "a.txt", 0, true},
	}
	for label, c := range cases {
		var a Attachment
		if err := json.Unmarshal([]byte(c.in), &a); err != nil {
			t.Errorf("%s: %v", label, err)
			continue
		}
		if a.Name != c.name || a.Size != c.size || a.IsUploaded != c.uploaded {
			t.Errorf("%s: got name %q size %d uploaded %v, want %q %d %v", label, a.Name, a.Size, a.IsUploaded, c.name, c.size, c.uploaded)
		}
	}
}

func TestProjectAndModuleDecode(t *testing.T) {
	cases := map[string]struct {
		in         string
		html, text string
	}{
		"html string":     {`{"description_html": "<p>x</p>", "description": "y"}`, "<p>x</p>", "y"},
		"html null":       {`{"description_html": null, "description": "line 1\nline 2"}`, "", "line 1\nline 2"},
		"html object":     {`{"description_html": {"type": "doc"}, "description": "y"}`, "", "y"},
		"html number":     {`{"description_html": 3}`, "", ""},
		"text null":       {`{"description_html": "<p>x</p>", "description": null}`, "<p>x</p>", ""},
		"text object":     {`{"description": {"type": "doc"}}`, "", ""},
		"both missing":    {`{}`, "", ""},
		"other fields ok": {`{"description": "y", "name": "N"}`, "", "y"},
	}
	for label, c := range cases {
		var p Project
		if err := json.Unmarshal([]byte(c.in), &p); err != nil {
			t.Errorf("project %s: %v", label, err)
		} else if p.DescriptionHTML != c.html || p.Description != c.text {
			t.Errorf("project %s: got %q, %q; want %q, %q", label, p.DescriptionHTML, p.Description, c.html, c.text)
		}
		var m Module
		if err := json.Unmarshal([]byte(c.in), &m); err != nil {
			t.Errorf("module %s: %v", label, err)
		} else if m.DescriptionHTML != c.html || m.Description != c.text {
			t.Errorf("module %s: got %q, %q; want %q, %q", label, m.DescriptionHTML, m.Description, c.html, c.text)
		}
	}
	var p Project
	if err := json.Unmarshal([]byte(`{"id": "p1", "identifier": "ALP", "name": "Alpha", "workspace": "ws1"}`), &p); err != nil ||
		p.ID != "p1" || p.Identifier != "ALP" || p.Name != "Alpha" || p.Workspace != "ws1" {
		t.Errorf("project fields = %+v, %v", p, err)
	}
}

func TestRelationRefDecode(t *testing.T) {
	var rels Relations
	in := `{"blocking": [{"project_id": "p1", "issue_id": "i1"}, "i2", {"issue_id": "i3"}], "relates_to": []}`
	if err := json.Unmarshal([]byte(in), &rels); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := []RelationRef{{ProjectID: "p1", IssueID: "i1"}, {IssueID: "i2"}, {IssueID: "i3"}}
	if got := rels["blocking"]; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("blocking = %+v, want %+v", got, want)
	}
	var ref RelationRef
	if err := json.Unmarshal([]byte(`42`), &ref); err == nil {
		t.Errorf("a number decoded as %+v, want an error", ref)
	}
}
