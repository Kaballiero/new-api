package domain

import "strings"

type ChannelRouting struct {
	ID       int
	Models   string
	Status   int
	Group    string
	Priority *int64
	Weight   *uint
	Tag      *string
}

func (c ChannelRouting) GetModels() []string {
	if c.Models == "" {
		return []string{}
	}
	return strings.Split(strings.Trim(c.Models, ","), ",")
}

type MetadataDeletion interface {
	ReadModels([]int) ([]Model, error)
	RoutingChannels() ([]ChannelRouting, error)
	SaveRouting(ChannelRouting) error
	DeleteModels([]int) error
	RemovePrices(map[string]struct{})
}
