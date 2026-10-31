package championship

import (
	"net/http"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// SkinDirectory is what the championship needs to know about user skins. It is implemented by skins.Manager.
type SkinDirectory interface {
	// UsableSkins returns, per car, the private and team skins (folder names) an account can use.
	UsableSkins(account uuid.UUID) (map[string][]string, error)
	// CheckUsable fails if the skin is a private or team skin the account may not use.
	CheckUsable(car, skin string, account uuid.UUID) error
}

// SetSkinDirectory tells the manager where the user skins are. Without it sign ups only offer the skins of the cars.
func (cm *ChampionshipManager) SetSkinDirectory(skins SkinDirectory) {
	cm.skins = skins
}

// skinAccountID is the id that owns skins for the account of a request: none for visitors without a login.
func skinAccountID(r *http.Request) uuid.UUID {
	account := smweb.AccountFromRequest(r)

	if !smweb.LoggedIn(r)() || account.ID == uuid.Nil {
		return uuid.Nil
	}

	return account.ID
}

// SignUpCars returns the skins of every car that the account can choose on the sign up form: the skins of the car
// plus its own and its teams' private skins.
func (cm *ChampionshipManager) SignUpCars(cars map[string][]string, account uuid.UUID) (map[string][]string, error) {
	if cm.skins == nil || account == uuid.Nil {
		return cars, nil
	}

	usable, err := cm.skins.UsableSkins(account)

	if err != nil {
		return nil, err
	}

	merged := make(map[string][]string, len(cars))

	for car, skins := range cars {
		merged[car] = append(append([]string(nil), skins...), usable[car]...)
	}

	return merged, nil
}

// checkSignUpSkin refuses a private or team skin that the registering account may not use.
func (cm *ChampionshipManager) checkSignUpSkin(response *smmodel.ChampionshipSignUpResponse, account uuid.UUID) error {
	if cm.skins == nil {
		return nil
	}

	if err := cm.skins.CheckUsable(response.Car, response.Skin, account); err != nil {
		return ValidationError("You cannot use the skin you picked. Please choose a different skin.")
	}

	return nil
}
