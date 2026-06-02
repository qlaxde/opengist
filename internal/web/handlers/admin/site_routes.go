package admin

import (
	"errors"
	"strconv"
	"strings"

	"github.com/thomiceli/opengist/internal/db"
	"github.com/thomiceli/opengist/internal/i18n"
	"github.com/thomiceli/opengist/internal/validator"
	"github.com/thomiceli/opengist/internal/web/context"
	"github.com/thomiceli/opengist/internal/web/siteroute"
)

// siteRouteGist resolves and validates the target gist for a route DTO. On
// failure it returns an error whose message is an i18n key describing why
// (gist not found / not anonymously accessible) so callers can surface it.
var (
	errSiteRouteGistNotFound  = errors.New("flash.admin.site-route-gist-not-found")
	errSiteRouteGistNotPublic = errors.New("flash.admin.site-route-gist-not-public")
)

func siteRouteGist(dto *db.SiteRouteDTO) (*db.Gist, error) {
	gist, err := db.GetGist(dto.User, dto.Slug)
	if err != nil {
		return nil, errSiteRouteGistNotFound
	}
	if !gist.Private.AllowsAnonymousAccess() {
		return nil, errSiteRouteGistNotPublic
	}
	return gist, nil
}

func AdminSiteRoutes(ctx *context.Context) error {
	ctx.SetData("htmlTitle", ctx.TrH("admin.site_routes")+" - "+ctx.TrH("admin.admin_panel"))
	ctx.SetData("adminHeaderPage", "site-routes")

	routes, err := db.GetAllSiteRoutes()
	if err != nil {
		return ctx.ErrorRes(500, "Cannot get site routes", err)
	}

	ctx.SetData("siteRoutes", routes)
	return ctx.Html("admin_site_routes.html")
}

func AdminSiteRoutesCreate(ctx *context.Context) error {
	dto := new(db.SiteRouteDTO)
	if err := ctx.Bind(dto); err != nil {
		return ctx.ErrorRes(400, ctx.Tr("error.cannot-bind-data"), err)
	}
	normalizeSiteRouteDTO(dto)

	if err := ctx.Validate(dto); err != nil {
		ctx.AddFlash(validator.ValidationMessages(&err, ctx.GetData("locale").(*i18n.Locale)), "error")
		return ctx.RedirectTo("/admin-panel/site-routes")
	}

	gist, gerr := siteRouteGist(dto)
	if gerr != nil {
		ctx.AddFlash(ctx.Tr(gerr.Error()), "error")
		return ctx.RedirectTo("/admin-panel/site-routes")
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
			ctx.AddFlash(ctx.Tr("flash.admin.site-route-already-mapped"), "error")
			return ctx.RedirectTo("/admin-panel/site-routes")
		}
		return ctx.ErrorRes(500, "Cannot create site route", err)
	}

	if err := siteroute.Reload(); err != nil {
		return ctx.ErrorRes(500, "Cannot reload site routes", err)
	}

	ctx.AddFlash(ctx.Tr("flash.admin.site-route-created"), "success")
	return ctx.RedirectTo("/admin-panel/site-routes")
}

func AdminSiteRoutesUpdate(ctx *context.Context) error {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 64)
	route, err := db.GetSiteRouteByID(uint(id))
	if err != nil {
		return ctx.ErrorRes(500, "Cannot retrieve site route", err)
	}

	dto := new(db.SiteRouteDTO)
	if err := ctx.Bind(dto); err != nil {
		return ctx.ErrorRes(400, ctx.Tr("error.cannot-bind-data"), err)
	}
	normalizeSiteRouteDTO(dto)

	if err := ctx.Validate(dto); err != nil {
		ctx.AddFlash(validator.ValidationMessages(&err, ctx.GetData("locale").(*i18n.Locale)), "error")
		return ctx.RedirectTo("/admin-panel/site-routes")
	}

	gist, gerr := siteRouteGist(dto)
	if gerr != nil {
		ctx.AddFlash(ctx.Tr(gerr.Error()), "error")
		return ctx.RedirectTo("/admin-panel/site-routes")
	}

	route.Host = dto.Host
	route.PathPrefix = dto.PathPrefix
	route.GistID = gist.ID
	route.Revision = dto.Revision
	route.Enabled = dto.Enabled

	if err := route.Update(); err != nil {
		if db.IsUniqueConstraintViolation(err) {
			ctx.AddFlash(ctx.Tr("flash.admin.site-route-already-mapped"), "error")
			return ctx.RedirectTo("/admin-panel/site-routes")
		}
		return ctx.ErrorRes(500, "Cannot update site route", err)
	}

	if err := siteroute.Reload(); err != nil {
		return ctx.ErrorRes(500, "Cannot reload site routes", err)
	}

	ctx.AddFlash(ctx.Tr("flash.admin.site-route-updated"), "success")
	return ctx.RedirectTo("/admin-panel/site-routes")
}

func AdminSiteRoutesDelete(ctx *context.Context) error {
	id, _ := strconv.ParseUint(ctx.Param("id"), 10, 64)
	route, err := db.GetSiteRouteByID(uint(id))
	if err != nil {
		return ctx.ErrorRes(500, "Cannot retrieve site route", err)
	}

	if err := route.Delete(); err != nil {
		return ctx.ErrorRes(500, "Cannot delete this site route", err)
	}

	if err := siteroute.Reload(); err != nil {
		return ctx.ErrorRes(500, "Cannot reload site routes", err)
	}

	ctx.AddFlash(ctx.Tr("flash.admin.site-route-deleted"), "success")
	return ctx.RedirectTo("/admin-panel/site-routes")
}

// normalizeSiteRouteDTO canonicalizes admin input before validation/storage:
// host lowercased; path prefix gets a leading slash and no trailing slash
// ("" for root); revision "" for HEAD (case-insensitive), hashes lowercased.
func normalizeSiteRouteDTO(dto *db.SiteRouteDTO) {
	dto.Host = strings.ToLower(strings.TrimSpace(dto.Host))

	prefix := strings.TrimSpace(dto.PathPrefix)
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		prefix = "/" + prefix
	}
	dto.PathPrefix = prefix

	rev := strings.TrimSpace(dto.Revision)
	if strings.EqualFold(rev, "HEAD") {
		rev = ""
	}
	dto.Revision = strings.ToLower(rev)

	dto.User = strings.TrimSpace(dto.User)
	dto.Slug = strings.TrimSpace(dto.Slug)
}
