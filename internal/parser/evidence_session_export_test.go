package parser

// HoldRarSessionForTest supplies the explicit ordinary-IO barrier for the public
// BookContent lifetime regression. It is not part of the production parser API.
func HoldRarSessionForTest(arc Archive) func() {
	a := arc.(*RarArchive)
	a.mu.Lock()
	return a.mu.Unlock
}
