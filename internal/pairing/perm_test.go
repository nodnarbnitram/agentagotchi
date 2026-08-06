package pairing

import "os"

type storePerms struct {
	filePerm os.FileMode
	dirPerm  os.FileMode
}

func storeStat(s *Store) (storePerms, error) {
	fi, err := os.Stat(s.path())
	if err != nil {
		return storePerms{}, err
	}
	di, err := os.Stat(s.dir)
	if err != nil {
		return storePerms{}, err
	}
	return storePerms{filePerm: fi.Mode().Perm(), dirPerm: di.Mode().Perm()}, nil
}
