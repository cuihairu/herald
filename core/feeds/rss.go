package feeds

import (
	"encoding/xml"
	"fmt"
	"time"
)

// ChannelMeta is the RSS channel-level metadata a feed carries. Link is
// optional in this package but the config layer gives every feed a
// site link; readers use it as the feed's home.
type ChannelMeta struct {
	Title       string
	Link        string
	Description string
}

// rssXML mirrors RSS 2.0 tightly enough for encoding/xml: channel with
// title/link/description/lastBuildDate and item entries. Fields stay
// flat — the renderer never emits namespaces.
type rssXML struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel channelXML `xml:"channel"`
}

type channelXML struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link,omitempty"`
	Description string    `xml:"description,omitempty"`
	LastBuild   string    `xml:"lastBuildDate"`
	Items       []itemXML `xml:"item"`
}

type itemXML struct {
	Title       string `xml:"title"`
	Link        string `xml:"link,omitempty"`
	Description string `xml:"description,omitempty"`
	GUID        guidXML `xml:"guid"`
	PubDate     string `xml:"pubDate"`
}

type guidXML struct {
	IsPermaLink bool   `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

// RenderRSS serializes one feed: metadata, the build time and the items
// (newest first, newestFirst order). Text goes through encoding/xml, so
// titles and bodies with markup or entity characters stay escaped; the
// notification id doubles as a non-permalink guid, which is what makes
// a re-pull of the same item a no-op for readers.
func RenderRSS(meta ChannelMeta, items []Item, now time.Time) (string, error) {
	if meta.Title == "" {
		return "", fmt.Errorf("feeds: channel title cannot be empty")
	}
	ch := channelXML{
		Title:       meta.Title,
		Link:        meta.Link,
		Description: meta.Description,
		LastBuild:   now.UTC().Format(time.RFC1123Z),
	}
	for _, it := range items {
		ch.Items = append(ch.Items, itemXML{
			Title:       it.Title,
			Link:        meta.Link,
			Description: it.Body,
			GUID:        guidXML{IsPermaLink: false, Value: it.ID},
			PubDate:     it.PublishedAt.UTC().Format(time.RFC1123Z),
		})
	}
	out, err := xml.MarshalIndent(rssXML{Version: "2.0", Channel: ch}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("feeds: render rss: %w", err)
	}
	return xml.Header + string(out) + "\n", nil
}
