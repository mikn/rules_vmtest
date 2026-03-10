package agentpb

// ExecRequest asks the guest agent to execute a shell command.
type ExecRequest struct {
	Command string
	Timeout int // seconds, 0 = default (900s)
}

// ExecResponse contains the result of command execution.
type ExecResponse struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// FileRequest asks about a file on the guest.
type FileRequest struct {
	Path string
}

// FileResponse describes a guest file.
type FileResponse struct {
	Exists bool
	Size   int64
	IsDir  bool
}

// WriteFileRequest writes a file on the guest.
type WriteFileRequest struct {
	Path    string
	Content []byte
	Mode    int
}

// UnitRequest queries a systemd unit.
type UnitRequest struct {
	Unit string
}

// UnitResponse describes a systemd unit's state.
type UnitResponse struct {
	ActiveState string // "active", "inactive", "failed", ...
	SubState    string // "running", "dead", "exited", ...
}

// BackgroundRequest starts a process as a transient systemd unit.
// Argv is passed directly to systemd-run (no shell evaluation).
type BackgroundRequest struct {
	Argv []string
}

// BackgroundResponse contains the transient unit name.
type BackgroundResponse struct {
	Unit string
}

// PortRequest checks whether a TCP port is open on the guest.
type PortRequest struct {
	Port    int
	Address string // default "localhost"
}

// PortResponse indicates whether the port is open.
type PortResponse struct {
	Open bool
}
