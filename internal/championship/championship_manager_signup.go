package championship

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/haisum/recaptcha"

	"github.com/wernersbacher/assetto-server-manager/internal/acserver/serverconfig"
	smmodel "github.com/wernersbacher/assetto-server-manager/internal/model"
)

type ValidationError string

func (e ValidationError) Error() string {
	return string(e)
}

var steamGUIDRegex = regexp.MustCompile("^[0-9]{17}(;[0-9]{17})*$")

func (cm *ChampionshipManager) HandleChampionshipSignUp(r *http.Request) (response *smmodel.ChampionshipSignUpResponse, foundSlot bool, err error) {
	if err := r.ParseForm(); err != nil {
		return nil, false, err
	}

	championship, err := cm.LoadChampionship(chi.URLParam(r, "championshipID"))

	if err != nil {
		return nil, false, err
	}

	signUpResponse := &smmodel.ChampionshipSignUpResponse{
		Created: time.Now(),
		Name:    strings.TrimSpace(r.FormValue("Name")),
		GUID:    serverconfig.NormaliseEntrantGUID(r.FormValue("GUID")),
		Team:    strings.TrimSpace(r.FormValue("Team")),
		Email:   strings.TrimSpace(r.FormValue("Email")),

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
			return signUpResponse, false, ValidationError("Please complete the reCAPTCHA.")
		}
	}

	if !steamGUIDRegex.MatchString(signUpResponse.GUID) {
		return signUpResponse, false, ValidationError("Please enter a valid SteamID64.")
	}

	for _, entrant := range championship.SignUpForm.Responses {
		if championship.SignUpForm.AskForEmail && entrant.Email == signUpResponse.Email && entrant.GUID != signUpResponse.GUID {
			return signUpResponse, false, ValidationError("Someone has already registered with this email address.")
		}
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
