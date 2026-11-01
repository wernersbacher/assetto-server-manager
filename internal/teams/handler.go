package teams

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/wernersbacher/ACPitwall/internal/api"
	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smstore "github.com/wernersbacher/ACPitwall/internal/store"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
)

// Handler serves the team pages. The permissions are checked by the Manager, the handler only translates its
// answers into pages, messages and redirects.
type Handler struct {
	*smweb.BaseHandler

	manager *Manager
	store   smstore.Store

	// baseURL is the address of the server (config http.base_url), for the links in the API. It may be empty.
	baseURL string
}

func NewHandler(baseHandler *smweb.BaseHandler, manager *Manager, store smstore.Store, baseURL string) *Handler {
	return &Handler{BaseHandler: baseHandler, manager: manager, store: store, baseURL: baseURL}
}

// actorFor returns who is acting. ok is false for visitors who are not logged in (the dummy account of an open
// server is not a login). Accounts with write access are moderators of all teams.
func actorFor(r *http.Request) (actor Actor, ok bool) {
	account := smweb.AccountFromRequest(r)

	if !smweb.LoggedIn(r)() || account.ID == uuid.Nil {
		return Actor{}, false
	}

	return Actor{AccountID: account.ID, Moderator: smweb.WriteAccess(r)()}, true
}

func (h *Handler) requireActor(w http.ResponseWriter, r *http.Request) (Actor, bool) {
	actor, ok := actorFor(r)

	if !ok {
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.teams.sign_in"))
		http.Redirect(w, r, "/login", http.StatusFound)
	}

	return actor, ok
}

// teamErrorMessage turns the errors of the team rules into a message for the user (in the language of the request).
// Other errors are not known.
func teamErrorMessage(r *http.Request, err error) (string, bool) {
	switch err {
	case smmodel.ErrTeamNotFound:
		return smweb.T(r, "flash.teams.not_found"), true
	case smmodel.ErrTeamNameInvalid:
		return smweb.T(r, "flash.teams.name_invalid", "min", smmodel.TeamNameMinLength, "max", smmodel.TeamNameMaxLength), true
	case smmodel.ErrTeamNameTaken:
		return smweb.T(r, "flash.teams.name_taken"), true
	case smmodel.ErrNotTeamMember:
		return smweb.T(r, "flash.teams.not_member"), true
	case smmodel.ErrAlreadyTeamMember:
		return smweb.T(r, "flash.teams.already_member"), true
	case smmodel.ErrTeamInviteInvalid:
		return smweb.T(r, "flash.teams.invite_invalid"), true
	case smmodel.ErrTeamPermission:
		return smweb.T(r, "flash.teams.not_allowed"), true
	case smmodel.ErrTeamOwnerRole:
		return smweb.T(r, "flash.teams.owner_role"), true
	case smmodel.ErrTeamTransferToSelf:
		return smweb.T(r, "flash.teams.already_owner"), true
	case smmodel.ErrTeamLimitReached:
		return smweb.T(r, "flash.teams.limit_reached"), true
	case smmodel.ErrTeamCreationBlocked:
		return smweb.T(r, "flash.teams.creation_blocked"), true
	case smstore.ErrAccountNotFound:
		return smweb.T(r, "flash.teams.account_not_found"), true
	}

	return "", false
}

// done flashes the result of an action (success is the translated message) and redirects. Unknown errors are server
// errors.
func (h *Handler) done(w http.ResponseWriter, r *http.Request, err error, success, redirect string) {
	if renamed, ok := err.(ErrRenameHook); ok {
		// the rename itself worked, only the registrations of running championships were not updated
		logrus.WithError(renamed.Err).Error("could not update the team name in the championships")
		smweb.AddFlash(w, r, success)
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.teams.rename_hook_failed"))
		http.Redirect(w, r, redirect, http.StatusFound)

		return
	}

	if err != nil {
		message, known := teamErrorMessage(r, err)

		if !known {
			logrus.WithError(err).Error("couldn't handle team request")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

			return
		}

		smweb.AddErrorFlash(w, r, message)
	} else if success != "" {
		smweb.AddFlash(w, r, success)
	}

	http.Redirect(w, r, redirect, http.StatusFound)
}

func teamPath(r *http.Request) string {
	return "/teams/" + chi.URLParam(r, "teamID")
}

func (h *Handler) accountNames() (map[uuid.UUID]string, error) {
	accounts, err := h.store.ListAccounts()

	if err != nil {
		return nil, err
	}

	names := make(map[uuid.UUID]string, len(accounts))

	for _, account := range accounts {
		names[account.ID] = displayName(account)
	}

	return names, nil
}

// displayName is how an account shows up for other people: the driver name if there is one.
func displayName(account *smmodel.Account) string {
	if account.DriverName != "" {
		return account.DriverName
	}

	return account.Name
}

type teamSummary struct {
	Team    *smmodel.Team
	Members int
	Role    smmodel.TeamRole
	Owner   string
}

type listTemplateVars struct {
	smweb.BaseTemplateVars

	LoggedIn  bool
	CanCreate bool

	MyTeams     []*teamSummary
	Invitations []*teamSummary
	OtherTeams  []*teamSummary

	// Query is the search text, it filters all lists by team name. OtherTeams is paginated.
	Query       string
	CurrentPage int
	NumPages    int
}

// teamsPageSize is the number of teams on one page of the list of all teams.
const teamsPageSize = 25

func filterSummaries(summaries []*teamSummary, query string) []*teamSummary {
	query = strings.ToLower(strings.TrimSpace(query))

	if query == "" {
		return summaries
	}

	var filtered []*teamSummary

	for _, summary := range summaries {
		if strings.Contains(strings.ToLower(summary.Team.Name), query) || strings.Contains(strings.ToLower(summary.Owner), query) {
			filtered = append(filtered, summary)
		}
	}

	return filtered
}

// applyQuery filters the lists by the search text and shows one (zero based) page of the other teams.
func (v *listTemplateVars) applyQuery(query string, page int) {
	v.Query = strings.TrimSpace(query)
	v.MyTeams = filterSummaries(v.MyTeams, query)
	v.Invitations = filterSummaries(v.Invitations, query)
	v.OtherTeams = filterSummaries(v.OtherTeams, query)

	v.NumPages = (len(v.OtherTeams) + teamsPageSize - 1) / teamsPageSize

	if page >= v.NumPages {
		page = v.NumPages - 1
	}

	if page < 0 {
		page = 0
	}

	v.CurrentPage = page

	start := page * teamsPageSize
	end := start + teamsPageSize

	if end > len(v.OtherTeams) {
		end = len(v.OtherTeams)
	}

	v.OtherTeams = v.OtherTeams[start:end]
}

func summarise(team *smmodel.Team, viewer uuid.UUID, names map[uuid.UUID]string) *teamSummary {
	summary := &teamSummary{Team: team, Members: len(team.Members)}

	if member := team.Member(viewer); member != nil {
		summary.Role = member.Role
	}

	if owner := team.Owner(); owner != nil {
		summary.Owner = names[owner.AccountID]
	}

	return summary
}

// buildList sorts the teams the actor can see into their own teams, the ones that invited them and the others.
func (h *Handler) buildList(actor Actor, loggedIn bool) (*listTemplateVars, error) {
	teams, err := h.manager.List(actor)

	if err != nil {
		return nil, err
	}

	names, err := h.accountNames()

	if err != nil {
		return nil, err
	}

	vars := &listTemplateVars{LoggedIn: loggedIn, CanCreate: loggedIn && h.manager.CanCreate(actor)}
	now := h.manager.now()

	for _, team := range teams {
		summary := summarise(team, actor.AccountID, names)

		switch {
		case team.IsMember(actor.AccountID):
			vars.MyTeams = append(vars.MyTeams, summary)
		case team.IsPrivate() && team.CanSee(actor.AccountID, now) && actor.AccountID != uuid.Nil && hasInviteFor(team, actor.AccountID, now):
			vars.Invitations = append(vars.Invitations, summary)
		default:
			vars.OtherTeams = append(vars.OtherTeams, summary)
		}
	}

	return vars, nil
}

func hasInviteFor(team *smmodel.Team, account uuid.UUID, now time.Time) bool {
	for _, invite := range team.Invites {
		if invite.AccountID == account && !invite.Expired(now) {
			return true
		}
	}

	return false
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	actor, loggedIn := actorFor(r)

	vars, err := h.buildList(actor, loggedIn)

	if err != nil {
		logrus.WithError(err).Error("couldn't list teams")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	vars.applyQuery(r.URL.Query().Get("q"), smweb.FormValueAsInt(r.URL.Query().Get("page")))

	h.ViewRenderer.MustLoadTemplate(w, r, "teams/list.html", vars)
}

type memberView struct {
	AccountID uuid.UUID
	Name      string
	Role      smmodel.TeamRole
	Joined    time.Time

	IsYou       bool
	CanKick     bool
	CanSetAdmin bool
	CanTransfer bool
}

type inviteView struct {
	Token   string
	Target  string
	Expires time.Time
	Path    string
}

type viewTemplateVars struct {
	smweb.BaseTemplateVars

	Team *smmodel.Team

	LoggedIn    bool
	IsMember    bool
	IsOwner     bool
	CanManage   bool
	IsModerator bool
	Invited     bool
	CanJoin     bool

	Members []*memberView
	Invites []*inviteView
}

// buildView prepares the team page for the actor, including what the actor may do with each member.
func (h *Handler) buildView(actor Actor, loggedIn bool, team *smmodel.Team) (*viewTemplateVars, error) {
	names, err := h.accountNames()

	if err != nil {
		return nil, err
	}

	now := h.manager.now()
	isOwner := team.IsOwner(actor.AccountID)
	isMember := team.IsMember(actor.AccountID)
	manages := actor.Moderator || team.CanManage(actor.AccountID)

	vars := &viewTemplateVars{
		Team:        team,
		LoggedIn:    loggedIn,
		IsMember:    isMember,
		IsOwner:     isOwner,
		CanManage:   manages,
		IsModerator: actor.Moderator,
		Invited:     hasInviteFor(team, actor.AccountID, now),
	}

	vars.CanJoin = loggedIn && !isMember && (!team.IsPrivate() || vars.Invited)

	for _, member := range team.Members {
		isYou := member.AccountID == actor.AccountID

		vars.Members = append(vars.Members, &memberView{
			AccountID: member.AccountID,
			Name:      names[member.AccountID],
			Role:      member.Role,
			Joined:    member.Joined,
			IsYou:     isYou,

			CanKick: !isYou && (actor.Moderator ||
				(manages && member.Role != smmodel.TeamRoleOwner && (member.Role != smmodel.TeamRoleAdmin || isOwner))),
			CanSetAdmin: member.Role != smmodel.TeamRoleOwner && (actor.Moderator || isOwner),
			CanTransfer: member.Role != smmodel.TeamRoleOwner && (actor.Moderator || isOwner),
		})
	}

	// owner, admins, members; people of the same role in the order they joined
	sort.SliceStable(vars.Members, func(i, j int) bool {
		if a, b := roleOrder(vars.Members[i].Role), roleOrder(vars.Members[j].Role); a != b {
			return a < b
		}

		return vars.Members[i].Joined.Before(vars.Members[j].Joined)
	})

	// only people who manage the team see its invites (the tokens are secret links)
	if manages {
		for _, invite := range team.Invites {
			if invite.Expired(now) {
				continue
			}

			view := &inviteView{Token: invite.Token, Expires: invite.Expires}

			if invite.AccountID != uuid.Nil {
				view.Target = names[invite.AccountID]
			} else {
				view.Path = "/teams/invite/" + invite.Token
			}

			vars.Invites = append(vars.Invites, view)
		}
	}

	return vars, nil
}

func roleOrder(role smmodel.TeamRole) int {
	switch role {
	case smmodel.TeamRoleOwner:
		return 0
	case smmodel.TeamRoleAdmin:
		return 1
	}

	return 2
}

func (h *Handler) View(w http.ResponseWriter, r *http.Request) {
	actor, loggedIn := actorFor(r)

	team, err := h.manager.Get(actor, chi.URLParam(r, "teamID"))

	if err == smmodel.ErrTeamNotFound {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Error("couldn't load team")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	vars, err := h.buildView(actor, loggedIn, team)

	if err != nil {
		logrus.WithError(err).Error("couldn't build team page")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	h.ViewRenderer.MustLoadTemplate(w, r, "teams/view.html", vars)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	visibility := smmodel.TeamPublic

	if r.FormValue("visibility") == string(smmodel.TeamPrivate) {
		visibility = smmodel.TeamPrivate
	}

	team, err := h.manager.Create(actor, r.FormValue("name"), visibility)

	if err != nil {
		h.done(w, r, err, "", "/teams")
		return
	}

	h.done(w, r, nil, smweb.T(r, "flash.teams.created"),"/teams/"+team.ID.String())
}

func (h *Handler) Join(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	_, err := h.manager.Join(actor, chi.URLParam(r, "teamID"), "")

	if err != nil {
		h.done(w, r, err, "", "/teams")
		return
	}

	h.done(w, r, nil, smweb.T(r, "flash.teams.joined"), teamPath(r))
}

func (h *Handler) Leave(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	h.done(w, r, h.manager.Leave(actor, chi.URLParam(r, "teamID")), smweb.T(r, "flash.teams.left"), "/teams")
}

func (h *Handler) Rename(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	_, err := h.manager.Rename(actor, chi.URLParam(r, "teamID"), r.FormValue("name"))

	h.done(w, r, err, smweb.T(r, "flash.teams.renamed"),teamPath(r))
}

func (h *Handler) SetVisibility(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	visibility := smmodel.TeamPublic

	if r.FormValue("visibility") == string(smmodel.TeamPrivate) {
		visibility = smmodel.TeamPrivate
	}

	_, err := h.manager.SetVisibility(actor, chi.URLParam(r, "teamID"), visibility)

	h.done(w, r, err, smweb.T(r, "flash.teams.visibility_changed"),teamPath(r))
}

// Invite invites the account with the name of the form value "driver", or creates an invite link if it is empty.
func (h *Handler) Invite(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	target := uuid.Nil

	if name := strings.TrimSpace(r.FormValue("driver")); name != "" {
		account, err := h.store.FindAccountByName(name)

		if err != nil {
			h.done(w, r, err, "", teamPath(r))
			return
		}

		target = account.ID
	}

	_, err := h.manager.Invite(actor, chi.URLParam(r, "teamID"), target)

	if target == uuid.Nil {
		h.done(w, r, err, smweb.T(r, "flash.teams.invite_link_created"),teamPath(r))
	} else {
		h.done(w, r, err, smweb.T(r, "flash.teams.invite_sent"),teamPath(r))
	}
}

func (h *Handler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	h.done(w, r, h.manager.RevokeInvite(actor, chi.URLParam(r, "teamID"), r.FormValue("token")), smweb.T(r, "flash.teams.invite_withdrawn"),teamPath(r))
}

func (h *Handler) DeclineInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	h.done(w, r, h.manager.DeclineInvite(actor, chi.URLParam(r, "teamID")), smweb.T(r, "flash.teams.invite_declined"),"/teams")
}

func (h *Handler) memberAction(w http.ResponseWriter, r *http.Request, do func(actor Actor, team string, member uuid.UUID) error, success string) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	member, err := uuid.Parse(chi.URLParam(r, "accountID"))

	if err != nil {
		h.done(w, r, smmodel.ErrNotTeamMember, "", teamPath(r))
		return
	}

	h.done(w, r, do(actor, chi.URLParam(r, "teamID"), member), success, teamPath(r))
}

func (h *Handler) Kick(w http.ResponseWriter, r *http.Request) {
	h.memberAction(w, r, h.manager.Kick, smweb.T(r, "flash.teams.member_removed"))
}

func (h *Handler) SetAdmin(w http.ResponseWriter, r *http.Request) {
	admin := r.FormValue("admin") == "on"

	h.memberAction(w, r, func(actor Actor, team string, member uuid.UUID) error {
		_, err := h.manager.SetAdmin(actor, team, member, admin)

		return err
	}, smweb.T(r, "flash.teams.role_changed"))
}

func (h *Handler) Transfer(w http.ResponseWriter, r *http.Request) {
	h.memberAction(w, r, func(actor Actor, team string, member uuid.UUID) error {
		_, err := h.manager.TransferOwnership(actor, team, member)

		return err
	}, smweb.T(r, "flash.teams.ownership_transferred"))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	h.done(w, r, h.manager.Delete(actor, chi.URLParam(r, "teamID")), smweb.T(r, "flash.teams.deleted"),"/teams")
}

type inviteTemplateVars struct {
	smweb.BaseTemplateVars

	Team     *smmodel.Team
	Token    string
	LoggedIn bool
	IsMember bool
}

// InvitePage shows the team an invite link leads to and lets the visitor accept it. Joining is a POST, so a link
// someone clicks by accident does not change anything.
func (h *Handler) InvitePage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	team, err := h.manager.FindByInvite(token)

	if err == smmodel.ErrTeamInviteInvalid {
		smweb.AddErrorFlash(w, r, smweb.T(r, "flash.teams.invite_invalid"))
		http.Redirect(w, r, "/teams", http.StatusFound)

		return
	} else if err != nil {
		logrus.WithError(err).Error("couldn't look up the invite")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	actor, loggedIn := actorFor(r)

	h.ViewRenderer.MustLoadTemplate(w, r, "teams/invite.html", &inviteTemplateVars{
		Team:     team,
		Token:    token,
		LoggedIn: loggedIn,
		IsMember: team.IsMember(actor.AccountID),
	})
}

func (h *Handler) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.requireActor(w, r)

	if !ok {
		return
	}

	team, err := h.manager.JoinByToken(actor, chi.URLParam(r, "token"))

	if err != nil {
		h.done(w, r, err, "", "/teams")
		return
	}

	h.done(w, r, nil, smweb.T(r, "flash.teams.joined"), "/teams/"+team.ID.String())
}

// ListAPI lists the teams the requester may see in the API format.
func (h *Handler) ListAPI(w http.ResponseWriter, r *http.Request) {
	actor, _ := actorFor(r)

	teams, err := h.manager.List(actor)

	if err != nil {
		logrus.WithError(err).Error("couldn't list teams")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	names, err := h.accountNames()

	if err != nil {
		logrus.WithError(err).Error("couldn't list accounts")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	out := make([]*api.Team, 0, len(teams))

	for _, team := range teams {
		out = append(out, api.NewTeam(team, names, h.baseURL))
	}

	api.WriteJSON(w, out)
}

// ViewAPI returns a team with its members in the API format. A private team does not exist for people who may not
// see it.
func (h *Handler) ViewAPI(w http.ResponseWriter, r *http.Request) {
	actor, _ := actorFor(r)

	team, err := h.manager.Get(actor, chi.URLParam(r, "teamID"))

	if err == smmodel.ErrTeamNotFound {
		http.NotFound(w, r)
		return
	} else if err != nil {
		logrus.WithError(err).Error("couldn't load team")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	names, err := h.accountNames()

	if err != nil {
		logrus.WithError(err).Error("couldn't list accounts")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	api.WriteJSON(w, api.NewTeamDetail(team, names, h.baseURL))
}
