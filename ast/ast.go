package ast

type NodeType uint16

const (
	NodeDoc NodeType = iota
	NodeSection
	NodePara
	NodeHeading
	NodeThematicBreak
	NodeDiv
	NodeCodeBlock
	NodeRawBlock
	NodeBlockQuote
	NodeOrderedList
	NodeBulletList
	NodeTaskList
	NodeDefinitionList
	NodeTable
	NodeCaption
	NodeRow
	NodeCell
	NodeListItem
	NodeTaskListItem
	NodeDefinitionListItem
	NodeTerm
	NodeDefinition
	NodeFootnote
	NodeReference

	NodeStr
	NodeSoftBreak
	NodeHardBreak
	NodeNonBreakingSpace
	NodeSymb
	NodeVerbatim
	NodeRawInline
	NodeInlineMath
	NodeDisplayMath
	NodeUrl
	NodeEmail
	NodeFootnoteReference
	NodeSmartPunctuation
	NodeEmph
	NodeStrong
	NodeLink
	NodeImage
	NodeSpan
	NodeMark
	NodeSuperscript
	NodeSubscript
	NodeInsert
	NodeDelete
	NodeDoubleQuoted
	NodeSingleQuoted
)

type Node struct {
	Type       NodeType
	Start, End int32
	Child      int32
	Next       int32
	Attr       int32
	AttrCount  uint16
	Level      int16
	Data       uint32
}

type Attribute struct {
	KeyStart, KeyEnd int32
	ValStart, ValEnd int32
}

const (
	AttrKeyID     = -1
	AttrKeyClass  = -2
	AttrKeyFormat = -3
)

type EventType uint16

const (
	EvNone EventType = iota
	EvStr
	EvSoftBreak
	EvHardBreak
	EvNonBreakingSpace
	EvEscape
	EvSymb
	EvFootnoteReference
	EvSmartPunctuation
	EvOpenVerbatim
	EvCloseVerbatim
	EvRawInline
	EvInlineMath
	EvDisplayMath
	EvUrl
	EvEmail

	EvOpenAttributes
	EvOpenInlineAttributes
	EvCloseAttributes
	EvAttrIdMarker
	EvAttrClassMarker
	EvAttrKey
	EvAttrValue
	EvAttrEqualMarker
	EvAttrQuoteMarker
	EvAttrSpace
	EvComment

	EvText

	EvOpenPara
	EvClosePara
	EvOpenHeading
	EvCloseHeading
	EvOpenBlockQuote
	EvCloseBlockQuote
	EvOpenList
	EvCloseList
	EvOpenListItem
	EvCloseListItem
	EvOpenDiv
	EvCloseDiv
	EvOpenCodeBlock
	EvCloseCodeBlock
	EvOpenRawBlock
	EvCloseRawBlock
	EvOpenTable
	EvCloseTable
	EvOpenRow
	EvCloseRow
	EvOpenCell
	EvCloseCell
	EvOpenCaption
	EvCloseCaption
	EvOpenFootnote
	EvCloseFootnote
	EvOpenReferenceDefinition
	EvCloseReferenceDefinition

	EvOpenEmph
	EvCloseEmph
	EvOpenStrong
	EvCloseStrong
	EvOpenLinkText
	EvCloseLinkText
	EvOpenImageText
	EvCloseImageText
	EvOpenDestination
	EvCloseDestination
	EvOpenReference
	EvCloseReference
	EvOpenSpan
	EvCloseSpan
	EvOpenMark
	EvCloseMark
	EvOpenInsert
	EvCloseInsert
	EvOpenDelete
	EvCloseDelete
	EvOpenSuperscript
	EvCloseSuperscript
	EvOpenSubscript
	EvCloseSubscript
	EvOpenDoubleQuoted
	EvCloseDoubleQuoted
	EvOpenSingleQuoted
	EvCloseSingleQuoted

	EvBlankLine
	EvThematicBreak
)

type Event struct {
	Start, End int32
	Type       EventType
}

type Reference struct {
	LabelStart, LabelEnd int32
	DestStart, DestEnd   int32
	Attributes           []Attribute
}

type Document struct {
	Source          []byte
	Extra           []byte
	Nodes           []Node
	Attributes      []Attribute
	References      map[string]Reference
	UsedFootnotes   []string
	FootnoteContent map[string]int32
	Events          []Event
}

type ContainerType uint8

const (
	ContainerNone ContainerType = iota
	ContainerDoc
	ContainerBlockQuote
	ContainerListItem
	ContainerList
	ContainerPara
	ContainerHeading
	ContainerTable
	ContainerCaption
	ContainerFootnote
	ContainerDiv
	ContainerCodeBlock
	ContainerRawBlock
	ContainerReferenceDef
	ContainerThematicBreak
)

type ContainerData struct {
	Level            int16
	Marker           byte
	MarkerEnd        byte
	Ordered          bool
	Tight            bool
	BlankLines       bool
	Indent           int32
	MarkerWidth      int32
	FenceChar        byte
	FenceLen         int32
	IsTask           bool
	Checked          bool
	NextNumber       int
	MarkerAmbiguous  bool
	MarkerFirstChar  byte
	OpenListEventIdx int32
}

const (
	DataAlignLeft uint32 = 1 << iota
	DataAlignCenter
	DataAlignRight
	DataListTight
	DataListDecimal
	DataListLowerAlpha
	DataListUpperAlpha
	DataListLowerRoman
	DataListUpperRoman
	DataTaskChecked
	DataCellHeader
	DataNoAutoID
)
