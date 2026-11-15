package copyengine

import "os"

func commitFile(temp, target string, noReplace bool) error {
	if !noReplace {
		return os.Rename(temp, target)
	}
	return renameNoReplace(temp, target)
}
