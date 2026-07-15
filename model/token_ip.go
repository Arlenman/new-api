package model

import (
	"errors"
	"net"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type TokenIP struct {
	Id          int    `json:"id"`
	TokenId     int    `json:"token_id" gorm:"index;uniqueIndex:idx_token_ip,priority:1"`
	IP          string `json:"ip" gorm:"type:varchar(45);uniqueIndex:idx_token_ip,priority:2"`
	CountryCode string `json:"country_code,omitempty" gorm:"type:varchar(8)"`
	Region      string `json:"region,omitempty" gorm:"type:varchar(128)"`
	City        string `json:"city,omitempty" gorm:"type:varchar(128)"`
	CreatedTime int64  `json:"created_time" gorm:"bigint;index"`
}

type TokenIPView struct {
	IP          string `json:"ip"`
	CountryCode string `json:"country_code,omitempty"`
	Region      string `json:"region,omitempty"`
	City        string `json:"city,omitempty"`
	Private     bool   `json:"private,omitempty"`
}

func BuildTokenIPViews(records []TokenIP) []TokenIPView {
	if len(records) == 0 {
		return nil
	}
	views := make([]TokenIPView, 0, len(records))
	for _, record := range records {
		parsedIP := net.ParseIP(record.IP)
		isPrivate := parsedIP != nil && (common.IsPrivateIP(parsedIP) || parsedIP.IsPrivate() || !parsedIP.IsGlobalUnicast())
		views = append(views, TokenIPView{
			IP:          record.IP,
			CountryCode: record.CountryCode,
			Region:      record.Region,
			City:        record.City,
			Private:     isPrivate,
		})
	}
	return views
}

func NormalizeIPAddress(rawIP string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(rawIP))
	if ip == nil {
		return "", errors.New("IP 地址格式无效")
	}
	return ip.String(), nil
}

func RecordTokenIP(tokenId int, rawIP string) error {
	if tokenId <= 0 {
		return errors.New("tokenId 无效")
	}
	ip, err := NormalizeIPAddress(rawIP)
	if err != nil {
		return err
	}
	record := &TokenIP{
		TokenId:     tokenId,
		IP:          ip,
		CreatedTime: common.GetTimestamp(),
	}
	return DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "token_id"}, {Name: "ip"}},
		DoNothing: true,
	}).Create(record).Error
}

func GetTokenIPsByTokenIDs(tokenIds []int) (map[int][]TokenIP, error) {
	result := make(map[int][]TokenIP, len(tokenIds))
	if len(tokenIds) == 0 {
		return result, nil
	}
	var records []TokenIP
	err := DB.Where("token_id IN (?)", tokenIds).
		Order("created_time desc, id desc").
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		result[record.TokenId] = append(result[record.TokenId], record)
	}
	return result, nil
}

func GetTokenIPsByTokenIDsAndIPs(tokenIds []int, ips []string) ([]TokenIP, error) {
	if len(tokenIds) == 0 || len(ips) == 0 {
		return []TokenIP{}, nil
	}
	var records []TokenIP
	err := DB.Where("token_id IN (?) AND ip IN (?)", tokenIds, ips).Find(&records).Error
	return records, err
}

func UpdateTokenIPLocation(tokenId int, ip string, countryCode string, region string, city string) error {
	if tokenId <= 0 || ip == "" {
		return errors.New("密钥 IP 参数无效")
	}
	return DB.Model(&TokenIP{}).
		Where("token_id = ? AND ip = ?", tokenId, ip).
		Updates(map[string]any{
			"country_code": countryCode,
			"region":       region,
			"city":         city,
		}).Error
}

func deleteTokenIPsTx(tx *gorm.DB, tokenIds []int) error {
	if len(tokenIds) == 0 {
		return nil
	}
	return tx.Where("token_id IN (?)", tokenIds).Delete(&TokenIP{}).Error
}
