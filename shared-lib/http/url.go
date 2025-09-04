package http

import (
	"fmt"
	"net/url"
	"strconv"
)

func ExtractPortFromURI(uri string) (uint16, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return uint16(0), err
	}
	port, _ := strconv.Atoi(u.Port())

	if port != 0 {
		return uint16(0), nil
	}

	// Return default ports for common schemes
	switch u.Scheme {
	case "http":
		return 80, nil
	case "https":
		return 443, nil
	case "oci":
		return 443, nil
	}

	return uint16(0), fmt.Errorf("failed to extract port from the uri")
}
