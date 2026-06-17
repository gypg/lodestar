package remotesite

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/lingyuins/octopus/internal/db"
	"github.com/lingyuins/octopus/internal/hub"
	"github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/utils/log"
)

// FetchAndStoreAnnouncement fetches the announcement from a remote site and saves it.
func FetchAndStoreAnnouncement(ctx context.Context, siteID int) error {
	site, err := Get(ctx, siteID)
	if err != nil {
		return err
	}
	if !site.Enabled {
		return fmt.Errorf("site %d is disabled", siteID)
	}

	adapter, err := hub.Get(site.SiteType)
	if err != nil {
		return err
	}

	content, err := adapter.FetchAnnouncement(ctx, site)
	if err != nil {
		return fmt.Errorf("fetch announcement: %w", err)
	}
	if content == "" {
		return nil
	}

	tx := db.GetDB().WithContext(ctx).Begin()
	if err := tx.Where("remote_site_id = ?", siteID).Delete(&model.SiteAnnouncement{}).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("delete old announcements: %w", err)
	}
	record := model.SiteAnnouncement{
		RemoteSiteID: siteID,
		Content:      content,
		FetchedAt:    time.Now(),
	}
	if err := tx.Create(&record).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("store announcement: %w", err)
	}
	return tx.Commit().Error
}

// FetchAllAnnouncements fetches announcements for all enabled sites.
func FetchAllAnnouncements(ctx context.Context) int {
	var sites []model.RemoteSite
	if err := db.GetDB().WithContext(ctx).Where("enabled = ?", true).Find(&sites).Error; err != nil {
		log.Warnf("list sites for announcement fetch: %v", err)
		return 0
	}
	siteIDs := make([]int, 0, len(sites))
	for _, site := range sites {
		siteIDs = append(siteIDs, site.ID)
	}

	var count atomic.Int64
	forEachSiteConcurrent(ctx, siteIDs, func(ctx context.Context, siteID int) {
		if err := FetchAndStoreAnnouncement(ctx, siteID); err != nil {
			log.Warnf("fetch announcement for site %d: %v", siteID, err)
			return
		}
		count.Add(1)
	})
	return int(count.Load())
}

// ListAnnouncements returns all cached announcements, newest first.
func ListAnnouncements(ctx context.Context) ([]model.SiteAnnouncement, error) {
	var announcements []model.SiteAnnouncement
	if err := db.GetDB().WithContext(ctx).
		Order("fetched_at DESC").
		Find(&announcements).Error; err != nil {
		return nil, fmt.Errorf("list announcements: %w", err)
	}
	return announcements, nil
}

// ListAnnouncementsBySite returns announcements for a specific site.
func ListAnnouncementsBySite(ctx context.Context, siteID int) ([]model.SiteAnnouncement, error) {
	var announcements []model.SiteAnnouncement
	if err := db.GetDB().WithContext(ctx).
		Where("remote_site_id = ?", siteID).
		Order("fetched_at DESC").
		Find(&announcements).Error; err != nil {
		return nil, fmt.Errorf("list announcements for site %d: %w", siteID, err)
	}
	return announcements, nil
}
