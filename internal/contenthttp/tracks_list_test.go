package contenthttp

import (
	"fmt"
	"testing"

	smcontent "github.com/wernersbacher/ACPitwall/internal/content"
)

func TestFilterTracks(t *testing.T) {
	tracks := []smcontent.Track{
		{Name: "ks_monza", Layouts: []string{"junior", "gp"}},
		{Name: "ks_silverstone"},
		{Name: "imola"},
	}

	if got := filterTracks(tracks, "  "); len(got) != 3 {
		t.Fatalf("empty query keeps all tracks, got %d", len(got))
	}

	if got := filterTracks(tracks, "MONZA junior"); len(got) != 1 || got[0].Name != "ks_monza" {
		t.Fatalf("unexpected result %v", got)
	}

	if got := filterTracks(tracks, "nothing"); len(got) != 0 {
		t.Fatalf("expected no tracks, got %v", got)
	}
}

func TestPaginateTracks(t *testing.T) {
	var tracks []smcontent.Track

	for i := 0; i < 5; i++ {
		tracks = append(tracks, smcontent.Track{Name: fmt.Sprint("t", i)})
	}

	got, page, pages := paginateTracks(tracks, 1, 2)

	if len(got) != 2 || got[0].Name != "t2" || page != 1 || pages != 3 {
		t.Fatalf("unexpected page %v %d %d", got, page, pages)
	}

	got, page, _ = paginateTracks(tracks, 9, 2)

	if len(got) != 1 || page != 2 {
		t.Fatalf("page out of range must show the last page, got %v %d", got, page)
	}

	got, page, pages = paginateTracks(nil, 3, 2)

	if len(got) != 0 || page != 0 || pages != 0 {
		t.Fatalf("unexpected empty page %v %d %d", got, page, pages)
	}
}
