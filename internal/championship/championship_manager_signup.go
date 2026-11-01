package championship

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/haisum/recaptcha"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
	"github.com/wernersbacher/ACPitwall/internal/i18n"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// ValidationError is a problem with the input of a form, which is shown to the user in the language of the request
// (see smweb.ErrorText). Error returns the English text.
type ValidationError struct {
	i18n.Message
}

func (e ValidationError) Error() string {
	return e.Message.String()
}

var steamGUIDRegex = regexp.MustCompile("^[0-9]{17}(;[0-9]{17})*$")

// signUpLoggedIn tells whether the requester has a login (the dummy account of an open server is none).
func signUpLoggedIn(r *http.Request) bool {
	return smweb.LoggedIn(r)() && smweb.AccountFromRequest(r).ID != uuid.Nil
}

// CheckSignUpAccount tells whether the requester may register: they need a login (the dummy account of an open
// server is none) with a GUID, which only a login with Steam sets.
func CheckSignUpAccount(r *http.Request) error {
	if !signUpLoggedIn(r) {
		return ValidationError{i18n.Msg("validation.championship.login_required", "Please log in to register for a championship.")}
	}

	if smweb.AccountFromRequest(r).GUID == "" {
		return ValidationError{i18n.Msg("validation.championship.steam_required", "Please connect your account with Steam to register for a championship.")}
	}

	return nil
}

func (cm *ChampionshipManager) HandleChampionshipSignUp(r *http.Request) (response *smmodel.ChampionshipSignUpResponse, foundSlot bool, err error) {
	if err := r.ParseForm(); err != nil {
		return nil, false, err
	}

	if err := CheckSignUpAccount(r); err != nil {
		return nil, false, err
	}

	// the team limit and the lock of the team are checked against the registrations, which are then saved: nobody
	// else may register in between
	defer LockSignUps()()

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		return nil, false, err
	}

	// the team of the form is taken over as typed, so the form can be shown again with the values of the driver if
	// something is wrong; resolveSignUpTeam decides what really counts
	chosenTeamID, _ := uuid.Parse(strings.TrimSpace(r.FormValue("TeamID")))

	signUpResponse := &smmodel.ChampionshipSignUpResponse{
		Created: time.Now(),
		Name:    strings.TrimSpace(r.FormValue("Name")),
		// the GUID always comes from the account, a value of the form is ignored
		GUID:   serverconfig.NormaliseEntrantGUID(smweb.AccountFromRequest(r).GUID),
		Team:   strings.TrimSpace(r.FormValue("Team")),
		TeamID: chosenTeamID,
		Email:  strings.TrimSpace(r.FormValue("Email")),

		Car:  r.FormValue("Car"),
		Skin: r.FormValue("Skin"),

		Questions: make(map[string]string),
		Status:    smmodel.ChampionshipEntrantPending,
	}

	for index, question := range championship.SignUpForm.ExtraFields {
		signUpResponse.Questions[question] = r.FormValue(fmt.Sprintf("Question.%d", index))
	}

	if cm.config.Championships.RecaptchaConfig.SecretKey != "" {
		captcha := recaptcha.R{
			Secret: cm.config.Championships.RecaptchaConfig.SecretKey,
		}

		if !captcha.Verify(*r) {
			return signUpResponse, false, ValidationError{i18n.Msg("validation.championship.recaptcha", "Please complete the reCAPTCHA.")}
		}
	}

	if !steamGUIDRegex.MatchString(signUpResponse.GUID) {
		return signUpResponse, false, ValidationError{i18n.Msg("validation.championship.steam_id", "Please enter a valid SteamID64.")}
	}

	for _, entrant := range championship.SignUpForm.Responses {
		if championship.SignUpForm.AskForEmail && entrant.Email == signUpResponse.Email && entrant.GUID != signUpResponse.GUID {
			return signUpResponse, false, ValidationError{i18n.Msg("validation.championship.email_taken", "Someone has already registered with this email address.")}
		}
	}

	if err := cm.resolveSignUpTeam(championship, signUpResponse, r); err != nil {
		return signUpResponse, false, err
	}

	if err := cm.checkSignUpTeam(championship, signUpResponse); err != nil {
		return signUpResponse, false, err
	}

	if err := cm.checkSignUpSkin(signUpResponse, skinAccountID(r)); err != nil {
		return signUpResponse, false, err
	}

	if !championship.SignUpForm.RequiresApproval {
		// check to see if there is room in the entrylist for the user in their specific car
		foundSlot, _, err = cm.AddEntrantFromSessionData(championship, signUpResponse, true, championship.SignUpForm.HideCarChoice)

		if err != nil {
			return signUpResponse, foundSlot, err
		}

		if foundSlot {
			signUpResponse.Status = smmodel.ChampionshipEntrantAccepted
		} else {
			signUpResponse.Status = smmodel.ChampionshipEntrantRejected
		}
	}

	updatingRegistration := false

	for index, response := range championship.SignUpForm.Responses {
		if response.GUID == signUpResponse.GUID {
			championship.SignUpForm.Responses[index] = signUpResponse
			updatingRegistration = true
			break
		}
	}

	if !updatingRegistration {
		championship.SignUpForm.Responses = append(championship.SignUpForm.Responses, signUpResponse)
	}

	return signUpResponse, foundSlot, cm.UpsertChampionship(championship)
}
