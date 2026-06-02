package admin

import (
	"strconv"

	"github.com/thomiceli/opengist/internal/db"
	"github.com/thomiceli/opengist/internal/i18n"
	"github.com/thomiceli/opengist/internal/validator"
	"github.com/thomiceli/opengist/internal/web/context"
	"github.com/thomiceli/opengist/internal/web/siteroute"
)

// PAT-authenticated, admin-only JSON API for the site routing table. Mirrors
// the admin UI handlers but speaks JSON so instance tooling can manage host →
// gist mappings without an OIDC session. Routes are gated by tokenAuthRequired
// + adminRequired; binding a host is an instance-level trust decision.

type apiSiteRoute struct {
	ID         uint   `json:"id"`
	Host       string `json:"host"`
	PathPrefix string `json:"path_prefix"`
	User       string `json:"user"`
	Slug       string `json:"slug"`
	Revision   string `json:"revision"`
	Enabled    bool   `json:"enabled"`
	GistID     uint   `json:"gist_id"`
}

type apiSiteRouteInput struct {
	Host       string `json:"host"`
	PathPrefix string `json:"path_prefix"`
	User       string `json:"user"`
	Slug       string `json:"slug"`
	Revision   string `json:"revision"`
	Enabled    *bool  `json:"enabled"` // omitted -> true on create, unchanged on update
}

func toAPISiteRoute(r *db.SiteRoute) apiSiteRoute {
	out := apiSiteRoute{
		ID:         r.ID,
		Host:       r.Host,
		PathPrefix: r.PathPrefix,
		Revision:   r.Revision,
		Enabled:    r.Enabled,
		GistID:     r.GistID,
	}
	if r.Gist.ID != 0 {
		out.User = r.Gist.User.Username
		out.Slug = r.Gist.Identifier()
	}
	return out
}

func (in *apiSiteRouteInput) toDTO(enabledDefault bool) *db.SiteRouteDTO {
	dto := &db.SiteRouteDTO{
		Host:       in.Host,
		PathPrefix: in.PathPrefix,
		User:       in.User,
		Slug:       in.Slug,
		Revision:   in.Revision,
		Enabled:    enabledDefault,
	}
	if in.Enabled != nil {
		dto.Enabled = *in.Enabled
	}
	return dto
}

func APIListSiteRoutes(ctx *context.Context) error {
	routes, err := db.GetAllSiteRoutes()
	if err != nil {
		return ctx.ErrorRes(500, "could not list site routes", err)
	}
	out := make([]apiSiteRoute, 0, len(routes))
	for _, r := range routes {
		out = append(out, toAPISiteRoute(r))
	}
	return ctx.JsonWithCode(200, map[string]any{"site_routes": out})
}

func APICreateSiteRoute(ctx *context.Context) error {
	var in apiSiteRouteInput
	if err := ctx.Bind(&in); err != nil {
		return ctx.ErrorRes(400, "invalid JSON body", err)
	}

	dto := in.toDTO(true)
	normalizeSiteRouteDTO(dto)
	if err := ctx.Validate(dto); err != nil {
		return ctx.ErrorRes(400, validator.ValidationMessages(&err, ctx.GetData("locale").(*i18n.Locale)), err)
	}

	gist, gerr := siteRouteGist(dto)
	if gerr != nil {
		return ctx.ErrorRes(422, ctx.Tr(gerr.Error()), gerr)
	}

	route := &db.SiteRoute{
		Host:       dto.Host,
		PathPrefix: dto.PathPrefix,
		GistID:     gist.ID,
		Revision:   dto.Revision,
		Enabled:    dto.Enabled,
	}
	if err := route.Create(); err != nil {
		if db.IsUniqueConstraintViolation(err) {
			return ctx.ErrorRes(409, "host and path prefix already mapped", err)
		}
		return ctx.ErrorRes(500, "could not create site route", err)
	}

	if err := siteroute.Reload(); err != nil {
		return ctx.ErrorRes(500, "could not reload site routes", err)
	}

	full, err := db.GetSiteRouteByID(route.ID)
	if err != nil {
		return ctx.ErrorRes(500, "could not read created site route", err)
	}
	return ctx.JsonWithCode(201, toAPISiteRoute(full))
}

func APIUpdateSiteRoute(ctx *context.Context) error {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 64)
	route, err := db.GetSiteRouteByID(uint(id))
	if err != nil {
		return ctx.ErrorRes(404, "site route not found", err)
	}

	var in apiSiteRouteInput
	if err := ctx.Bind(&in); err != nil {
		return ctx.ErrorRes(400, "invalid JSON body", err)
	}

	dto := in.toDTO(route.Enabled)
	normalizeSiteRouteDTO(dto)
	if err := ctx.Validate(dto); err != nil {
		return ctx.ErrorRes(400, validator.ValidationMessages(&err, ctx.GetData("locale").(*i18n.Locale)), err)
	}

	gist, gerr := siteRouteGist(dto)
	if gerr != nil {
		return ctx.ErrorRes(422, ctx.Tr(gerr.Error()), gerr)
	}

	route.Host = dto.Host
	route.PathPrefix = dto.PathPrefix
	route.GistID = gist.ID
	route.Revision = dto.Revision
	route.Enabled = dto.Enabled

	if err := route.Update(); err != nil {
		if db.IsUniqueConstraintViolation(err) {
			return ctx.ErrorRes(409, "host and path prefix already mapped", err)
		}
		return ctx.ErrorRes(500, "could not update site route", err)
	}

	if err := siteroute.Reload(); err != nil {
		return ctx.ErrorRes(500, "could not reload site routes", err)
	}

	full, err := db.GetSiteRouteByID(route.ID)
	if err != nil {
		return ctx.ErrorRes(500, "could not read updated site route", err)
	}
	return ctx.JsonWithCode(200, toAPISiteRoute(full))
}

func APIDeleteSiteRoute(ctx *context.Context) error {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 64)
	route, err := db.GetSiteRouteByID(uint(id))
	if err != nil {
		return ctx.ErrorRes(404, "site route not found", err)
	}

	if err := route.Delete(); err != nil {
		return ctx.ErrorRes(500, "could not delete site route", err)
	}

	if err := siteroute.Reload(); err != nil {
		return ctx.ErrorRes(500, "could not reload site routes", err)
	}

	return ctx.JsonWithCode(200, map[string]any{"deleted": true, "id": uint(id)})
}
