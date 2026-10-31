package championship

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// fakeSkins knows one private skin of one account.
type fakeSkins struct {
	owner uuid.UUID
	car   string
	skin  string
}

func (f fakeSkins) UsableSkins(account uuid.UUID) (map[string][]string, error) {
	if account == f.owner {
		return map[string][]string{f.car: {f.skin}}, nil
	}

	return map[string][]string{}, nil
}

func (f fakeSkins) CheckUsable(car, skin string, account uuid.UUID) error {
	if car == f.car && skin == f.skin && account != f.owner {
		return errors.New("not yours")
	}

	return nil
}

func TestSignUpSkins(t *testing.T) {
	e := newSignUpTeamsEnv(t, selectForm())

	owner, _ := e.account()
	other, _ := e.account()

	e.cm.SetSkinDirectory(fakeSkins{owner: owner.ID, car: "car", skin: "u1234_mine"})

	// the driver is offered the own skins besides the skins of the car
	cars := map[string][]string{"car": {"red"}, "other": {"blue"}}

	got, err := e.cm.SignUpCars(cars, owner.ID)

	if err != nil || len(got["car"]) != 2 || got["car"][1] != "u1234_mine" || len(got["other"]) != 1 {
		t.Fatalf("own skins missing: %v %v", got, err)
	}

	if len(cars["car"]) != 1 {
		t.Error("the skins of the cars must not be changed")
	}

	if got, _ := e.cm.SignUpCars(cars, other.ID); len(got["car"]) != 1 {
		t.Errorf("a stranger sees no private skins: %v", got)
	}

	if got, _ := e.cm.SignUpCars(cars, uuid.Nil); len(got["car"]) != 1 {
		t.Errorf("a visitor sees no private skins: %v", got)
	}

	// registering with a skin of somebody else is refused, an anonymous visitor cannot use it either
	if _, err := e.signUp(other, e.guid(), map[string][]string{"Skin": {"u1234_mine"}}); err == nil {
		t.Error("a stranger must not register with a private skin")
	} else {
		expectValidation(t, "stranger", err, "cannot use the skin")
	}

	if _, err := e.signUp(nil, e.guid(), map[string][]string{"Skin": {"u1234_mine"}}); err == nil {
		t.Error("an anonymous visitor must not register with a private skin")
	}

	// the owner and every other skin are fine
	if _, err := e.signUp(owner, e.guid(), map[string][]string{"Skin": {"u1234_mine"}}); err != nil {
		t.Errorf("the owner can use the skin: %v", err)
	}

	if _, err := e.signUp(other, e.guid(), map[string][]string{"Skin": {"red"}}); err != nil {
		t.Errorf("a normal skin is fine: %v", err)
	}
}
