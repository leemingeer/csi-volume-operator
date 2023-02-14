package controllers

import "fmt"

func wrapError(wrapMessage string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %v", wrapMessage, err)
}
