package db

// SiteRoute maps an external host (+ optional path prefix) onto the rendered
// /site of a gist revision. It is an admin-managed instance-level table modelled
// on the Invitation feature: binding a hostname is a trust decision, not a
// per-gist self-service one.
//
// The FK is by GistID (numeric) so a route survives a gist rename, and CASCADE
// removes routes when the gist is deleted (prevents dangling routes / slug-reuse
// hijack). The rewrite into the existing /site route, however, is built from the
// gist's current Identifier() + owner username (see internal/web/siteroute).
type SiteRoute struct {
	ID         uint   `gorm:"primaryKey"`
	Host       string `gorm:"index:idx_site_route_host_prefix,unique"` // lowercased, no port
	PathPrefix string `gorm:"index:idx_site_route_host_prefix,unique"` // "" = root, else "/faq" (leading slash, no trailing)
	GistID     uint
	Gist       Gist   `gorm:"foreignKey:GistID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Revision   string // "" = HEAD; else commit hash
	Enabled    bool
	CreatedAt  int64
	UpdatedAt  int64
}

// GetAllSiteRoutes returns every route for the admin list, ordered for display.
func GetAllSiteRoutes() ([]*SiteRoute, error) {
	var routes []*SiteRoute
	err := db.
		Preload("Gist.User").
		Order("host ASC, path_prefix ASC").
		Find(&routes).Error
	return routes, err
}

func GetSiteRouteByID(id uint) (*SiteRoute, error) {
	route := new(SiteRoute)
	err := db.
		Preload("Gist.User").
		Where("id = ?", id).
		First(&route).Error
	return route, err
}

// GetSiteRoutesForMatching returns enabled routes ordered so that, within a host,
// the longest path prefix comes first (root catch-all last). The resolver relies
// on this ordering. LENGTH() is used for dialect-neutral length sorting.
func GetSiteRoutesForMatching() ([]*SiteRoute, error) {
	var routes []*SiteRoute
	err := db.
		Preload("Gist.User").
		Where("enabled = ?", true).
		Order("host ASC, LENGTH(path_prefix) DESC").
		Find(&routes).Error
	return routes, err
}

func (r *SiteRoute) Create() error {
	return db.Create(&r).Error
}

func (r *SiteRoute) Update() error {
	return db.Save(&r).Error
}

func (r *SiteRoute) Delete() error {
	return db.Delete(&r).Error
}

// -- DTO -- //

// SiteRouteDTO carries admin form input. Host/PathPrefix/Revision are normalized
// in the handler before validation (host lowercased; prefix gets a leading slash
// and no trailing slash, "" for root; revision "" for HEAD).
type SiteRouteDTO struct {
	Host       string `form:"host" validate:"required,max=253,sitehost"`
	PathPrefix string `form:"pathPrefix" validate:"siteprefix"`
	User       string `form:"user" validate:"required,max=24"`
	Slug       string `form:"slug" validate:"required,max=32"`
	Revision   string `form:"revision" validate:"siterevision"`
	Enabled    bool   `form:"enabled"`
}
