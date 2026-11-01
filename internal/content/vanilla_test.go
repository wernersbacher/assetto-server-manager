package content

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()

	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVanillaContentIsHiddenUntilUploaded(t *testing.T) {
	root, err := ioutil.TempDir("", "vanilla")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(root)

	// content that exists before the install counts as uploaded
	mkdirs(t, root, "content/cars/my_car", "content/tracks/my_track")
	before := TakeContentSnapshot(root)

	// what steamcmd brings
	mkdirs(t, root, "content/cars/ks_vanilla", "content/tracks/ks_vanilla_track")

	if err := MarkVanillaContent(root, before); err != nil {
		t.Fatal(err)
	}

	cm := NewCarManager(root, false, false)
	tm := NewTrackManager(root)

	cars, err := cm.ListCars()

	if err != nil || len(cars) != 1 || cars[0].Name != "my_car" {
		t.Fatalf("ListCars = %v, %v; want only my_car", cars, err)
	}

	tracks, err := tm.ListTracks()

	if err != nil || len(tracks) != 1 || tracks[0].Name != "my_track" {
		t.Fatalf("ListTracks = %v, %v; want only my_track", tracks, err)
	}

	all, err := cm.listCars(true)

	if err != nil || len(all) != 2 {
		t.Fatalf("listCars(true) = %v, %v; want both cars", all, err)
	}

	// uploading files of a vanilla car/track makes it visible
	if err := ForgetVanillaCar(root, "ks_vanilla"); err != nil {
		t.Fatal(err)
	}

	if err := ForgetVanillaTrack(root, "ks_vanilla_track"); err != nil {
		t.Fatal(err)
	}

	if cars, _ = cm.ListCars(); len(cars) != 2 {
		t.Fatalf("ListCars after upload = %d cars, want 2", len(cars))
	}

	if tracks, _ = tm.ListTracks(); len(tracks) != 2 {
		t.Fatalf("ListTracks after upload = %d tracks, want 2", len(tracks))
	}

	// an update of the install keeps what was uploaded and hides only what is new
	before = TakeContentSnapshot(root)
	mkdirs(t, root, "content/cars/ks_new")

	if err := MarkVanillaContent(root, before); err != nil {
		t.Fatal(err)
	}

	if cars, _ = cm.ListCars(); len(cars) != 2 {
		t.Fatalf("ListCars after update = %d cars, want 2 (ks_new hidden)", len(cars))
	}
}

func TestNoVanillaMarkerHidesNothing(t *testing.T) {
	root, err := ioutil.TempDir("", "vanilla")

	if err != nil {
		t.Fatal(err)
	}

	defer os.RemoveAll(root)

	mkdirs(t, root, "content/cars/a", "content/tracks/b")

	cars, err := NewCarManager(root, false, false).ListCars()

	if err != nil || len(cars) != 1 {
		t.Fatalf("ListCars = %v, %v; want 1 car", cars, err)
	}
}
