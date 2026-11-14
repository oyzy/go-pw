package dbdoc

// KeePass 2 ships a fixed set of 69 built-in icons. Databases only store the numeric
// identifier, so the identifiers must match KeePass exactly for the icons to look the
// same in both applications.
const (
	IconKey           = 0
	IconWorld         = 1
	IconWarning       = 2
	IconNetwork       = 3
	IconMarked        = 4
	IconMail          = 5
	IconInfo          = 6
	IconHome          = 7
	IconStar          = 8
	IconDisk          = 9
	IconCamera        = 10
	IconTrash         = 43
	IconFolder        = 48
	IconFolderOpen    = 49
	IconFolderPackage = 50
	IconTerminal      = 51
	IconConsole       = 52
	IconPrinter       = 53
	IconProgram       = 54
	IconKeyRing       = 55
	IconFolderLocked  = 56
	IconBox           = 57
)

// IconNames maps the KeePass icon identifiers to a short human readable name, matching
// the names used by KeePass 2's icon picker.
var IconNames = map[int]string{
	0:  "Key",
	1:  "World",
	2:  "Warning",
	3:  "NetworkServer",
	4:  "MarkedDirectory",
	5:  "UserCommunication",
	6:  "Parts",
	7:  "Notepad",
	8:  "WorldSocket",
	9:  "Identity",
	10: "PaperReady",
	11: "Digicam",
	12: "IRCommunication",
	13: "MultiKeys",
	14: "Energy",
	15: "Scanner",
	16: "WorldStar",
	17: "CDRom",
	18: "Monitor",
	19: "EMail",
	20: "Configuration",
	21: "ClipboardReady",
	22: "PaperNew",
	23: "Screen",
	24: "EnergyCareful",
	25: "EMailBox",
	26: "Disk",
	27: "Drive",
	28: "PaperQ",
	29: "TerminalEncrypted",
	30: "Console",
	31: "Printer",
	32: "ProgramIcons",
	33: "Run",
	34: "Settings",
	35: "WorldComputer",
	36: "Archive",
	37: "Homebanking",
	38: "DriveWindows",
	39: "Clock",
	40: "EMailSearch",
	41: "PaperFlag",
	42: "Memory",
	43: "TrashBin",
	44: "Note",
	45: "Expired",
	46: "Info",
	47: "Package",
	48: "Folder",
	49: "FolderOpen",
	50: "FolderPackage",
	51: "LockOpen",
	52: "PaperLocked",
	53: "Checked",
	54: "Pen",
	55: "Thumbnail",
	56: "Book",
	57: "List",
	58: "UserKey",
	59: "Tool",
	60: "Home",
	61: "Star",
	62: "Tux",
	63: "Feather",
	64: "Apple",
	65: "Wiki",
	66: "Money",
	67: "Certificate",
	68: "BlackBerry",
}

// MaxBuiltinIconID is the highest valid built-in icon identifier.
const MaxBuiltinIconID = 68
