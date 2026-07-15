package controller

import "errors"

var (
	errInvalidUpstreamChannelID       = errors.New("invalid upstream channel id")
	errInvalidUpstreamBaseURL         = errors.New("invalid upstream base URL")
	errInvalidUpstreamChannelName     = errors.New("upstream channel name must not exceed 255 characters")
	errInvalidUpstreamSelectedGroup   = errors.New("selected upstream group must not exceed 255 characters")
	errInvalidUpstreamKeyID           = errors.New("invalid upstream key id")
	errInvalidUpstreamProvider        = errors.New("provider must be auto, new-api, sub2api, or other")
	errInvalidUpstreamAuthType        = errors.New("authentication method must be password or access_token")
	errUpstreamAccessTokenProvider    = errors.New("management access token authentication is only supported for new-api upstream channels")
	errInvalidUpstreamUserID          = errors.New("management access token authentication requires a positive numeric upstream user ID")
	errUpstreamCredentialRequired     = errors.New("enter a new password or management access token when changing the authentication method")
	errInvalidUpstreamPriority        = errors.New("upstream channel priority must be between -2147483648 and 2147483647")
	errInvalidUpstreamCredential      = errors.New("upstream username or password is too long")
	errUpstreamCryptoSecretRequired   = errors.New("SESSION_SECRET or CRYPTO_SECRET must be configured before saving upstream passwords")
	errInvalidUpstreamThreshold       = errors.New("balance threshold must be between 0 and 1000000000")
	errInvalidUpstreamMultiplier      = errors.New("channel multiplier must be greater than 0, at most 1000000000, and have no more than 2 decimal places")
	errInvalidUpstreamRefreshInterval = errors.New("auto refresh interval must be 0 or between 60 and 86400 seconds")
	errInvalidUpstreamNote            = errors.New("upstream note must not exceed 2000 characters")
)
