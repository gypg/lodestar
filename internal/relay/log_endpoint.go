package relay

import dbmodel "github.com/lingyuins/octopus/internal/model"

func resolveRelayLogEndpointType(requestedEndpointType, matchedGroupEndpointType string) string {
	requested := dbmodel.NormalizeEndpointType(requestedEndpointType)
	matched := dbmodel.NormalizeEndpointType(matchedGroupEndpointType)

	if matched == "" || matched == dbmodel.EndpointTypeAll {
		return requested
	}

	return matched
}
