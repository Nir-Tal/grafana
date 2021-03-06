package guardian

import (
	"github.com/grafana/grafana/pkg/bus"
	"github.com/grafana/grafana/pkg/log"
	m "github.com/grafana/grafana/pkg/models"
	"github.com/grafana/grafana/pkg/setting"
)

// DashboardGuardian to be used for guard against operations without access on dashboard and acl
type DashboardGuardian interface {
	CanSave() (bool, error)
	CanEdit() (bool, error)
	CanView() (bool, error)
	CanAdmin() (bool, error)
	HasPermission(permission m.PermissionType) (bool, error)
	CheckPermissionBeforeUpdate(permission m.PermissionType, updatePermissions []*m.DashboardAcl) (bool, error)
	GetAcl() ([]*m.DashboardAclInfoDTO, error)
}

type dashboardGuardianImpl struct {
	user   *m.SignedInUser
	dashId int64
	orgId  int64
	acl    []*m.DashboardAclInfoDTO
	groups []*m.Team
	log    log.Logger
}

// New factory for creating a new dashboard guardian instance
var New = func(dashId int64, orgId int64, user *m.SignedInUser) DashboardGuardian {
	return &dashboardGuardianImpl{
		user:   user,
		dashId: dashId,
		orgId:  orgId,
		log:    log.New("guardians.dashboard"),
	}
}

func (g *dashboardGuardianImpl) CanSave() (bool, error) {
	return g.HasPermission(m.PERMISSION_EDIT)
}

func (g *dashboardGuardianImpl) CanEdit() (bool, error) {
	if setting.ViewersCanEdit {
		return g.HasPermission(m.PERMISSION_VIEW)
	}

	return g.HasPermission(m.PERMISSION_EDIT)
}

func (g *dashboardGuardianImpl) CanView() (bool, error) {
	return g.HasPermission(m.PERMISSION_VIEW)
}

func (g *dashboardGuardianImpl) CanAdmin() (bool, error) {
	return g.HasPermission(m.PERMISSION_ADMIN)
}

func (g *dashboardGuardianImpl) HasPermission(permission m.PermissionType) (bool, error) {
	if g.user.OrgRole == m.ROLE_ADMIN {
		return true, nil
	}

	acl, err := g.GetAcl()
	if err != nil {
		return false, err
	}

	return g.checkAcl(permission, acl)
}

func (g *dashboardGuardianImpl) checkAcl(permission m.PermissionType, acl []*m.DashboardAclInfoDTO) (bool, error) {
	orgRole := g.user.OrgRole
	teamAclItems := []*m.DashboardAclInfoDTO{}

	for _, p := range acl {
		// user match
		if !g.user.IsAnonymous {
			if p.UserId == g.user.UserId && p.Permission >= permission {
				return true, nil
			}
		}

		// role match
		if p.Role != nil {
			if *p.Role == orgRole && p.Permission >= permission {
				return true, nil
			}
		}

		// remember this rule for later
		if p.TeamId > 0 {
			teamAclItems = append(teamAclItems, p)
		}
	}

	// do we have team rules?
	if len(teamAclItems) == 0 {
		return false, nil
	}

	// load teams
	teams, err := g.getTeams()
	if err != nil {
		return false, err
	}

	// evalute team rules
	for _, p := range acl {
		for _, ug := range teams {
			if ug.Id == p.TeamId && p.Permission >= permission {
				return true, nil
			}
		}
	}

	return false, nil
}

func (g *dashboardGuardianImpl) CheckPermissionBeforeUpdate(permission m.PermissionType, updatePermissions []*m.DashboardAcl) (bool, error) {
	if g.user.OrgRole == m.ROLE_ADMIN {
		return true, nil
	}

	acl := []*m.DashboardAclInfoDTO{}

	for _, p := range updatePermissions {
		acl = append(acl, &m.DashboardAclInfoDTO{UserId: p.UserId, TeamId: p.TeamId, Role: p.Role, Permission: p.Permission})
	}

	return g.checkAcl(permission, acl)
}

// GetAcl returns dashboard acl
func (g *dashboardGuardianImpl) GetAcl() ([]*m.DashboardAclInfoDTO, error) {
	if g.acl != nil {
		return g.acl, nil
	}

	query := m.GetDashboardAclInfoListQuery{DashboardId: g.dashId, OrgId: g.orgId}
	if err := bus.Dispatch(&query); err != nil {
		return nil, err
	}

	g.acl = query.Result
	return g.acl, nil
}

func (g *dashboardGuardianImpl) getTeams() ([]*m.Team, error) {
	if g.groups != nil {
		return g.groups, nil
	}

	query := m.GetTeamsByUserQuery{OrgId: g.orgId, UserId: g.user.UserId}
	err := bus.Dispatch(&query)

	g.groups = query.Result
	return query.Result, err
}
