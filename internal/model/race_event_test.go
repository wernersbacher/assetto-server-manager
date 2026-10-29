package model

import "testing"

func TestRaceEvent_StoredNames(t *testing.T) {
	for name, event := range map[string]RaceEvent{
		"*servermanager.QuickRace":          &QuickRace{},
		"*servermanager.CustomRace":         &CustomRace{},
		"*servermanager.ActiveChampionship": &ActiveChampionship{},
		"*servermanager.ActiveRaceWeekend":  &ActiveRaceWeekend{},
	} {
		if got := nameOf(event); got != name {
			t.Errorf("name of %T is %q, expected stored name %q", event, got, name)
		}

		data, err := MarshalRaceEvent(event)

		if err != nil {
			t.Fatal(err)
		}

		decoded, err := UnmarshalRaceEvent(data)

		if err != nil {
			t.Fatalf("could not unmarshal %T: %v", event, err)
		}

		if nameOf(decoded) != name {
			t.Errorf("decoded %T, expected %s", decoded, name)
		}
	}
}

func TestUnmarshalRaceEvent_LegacyData(t *testing.T) {
	event, err := UnmarshalRaceEvent([]byte(`{"Name":"*servermanager.CustomRace","RaceEvent":{"Name":"Legacy"}}`))

	if err != nil {
		t.Fatal(err)
	}

	custom, ok := event.(*CustomRace)

	if !ok || custom.Name != "Legacy" {
		t.Fatalf("unexpected event %#v", event)
	}
}
