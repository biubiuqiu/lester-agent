package localrun

// os.Root enforces the Windows handle boundary. Identity checks after opening
// also reject a destination that was replaced with a reparse point.
const localFileReadFlags = 0
