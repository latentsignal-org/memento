# Message Reference and Preview Spec

Document status: proposed UI consolidation
Last updated: June 16, 2026
Audience: engineers working on source-message references, citations, and previews

## 1. Purpose

Memento shows source messages in several UI contexts:

1. As evidence markers inside generated writing.
2. As source references inside Ask Memento answers.
3. As message rows in supporting-message lists.
4. As hover previews.
5. As expanded inline previews.
6. As full supporting-message panels.

The current implementation uses separate components for these cases:

- `MessagePill` in `AgentChat.tsx`
- `CitationButton`, `CitationChipList`, and `EvidenceText` in `EvidenceCitations.tsx`
- `CitationHoverCard`
- `MessageRow`
- `MessagePreviewPanel`

The target architecture should keep the product semantics clear while making the visual language consistent. A source
reference is not always a citation, and a message preview is not the same thing as a citation marker.

## 2. Core vocabulary

### Message reference

A **message reference** is the clickable or hoverable UI element that points to a source message.

Examples:

- `[1]` in a project narrative
- `Atlas prototype review` in Ask Memento
- `Email #2043` when only the source message ID is useful

A message reference may or may not be a citation in the strict evidence sense.

### Citation

A **citation** is a message reference used as evidence for a generated claim.

Examples:

- A project paragraph ending with `[1] [2] [3]`
- A person facet backed by source-message chips
- A concept section citing source messages

Ask Memento references are sometimes citations, but not always. If the answer says a factual claim and includes
`[msg:2043]`, it is a citation. If the answer says "here are three matching messages" and lists message references, it
is better described as source-message results.

### Message preview

A **message preview** displays the message itself, or enough of it for inspection.

Examples:

- Compact card with sender, date, subject, and snippet
- Supporting Emails row in a project page
- Right-side Supporting email panel
- Inline expanded preview in review/group UIs

## 3. Component split

### `MessageReference`

`MessageReference` renders the source-message reference. It owns how the reference is displayed, and it may compose a
compact `MessagePreview` for preview behavior.

It does not render full message content itself.

```tsx
type MessageReferenceProps = {
  messageId: number;

  display: "citation-number" | "subject" | "message-id" | "link-text";

  // Required only when display="citation-number".
  citationNumber?: number | string;

  // Optional for display="link-text" or custom fallback copy.
  label?: string;

  // Preview is independent of what click does. Desktop can reveal compact
  // previews on hover/focus; touch UIs can reveal the same preview on tap.
  preview?: "none" | "compact";

  // What click or keyboard activation does.
  openTarget?: "none" | "right-panel" | "inline" | "external";

  // Optional message data when the caller already has it. If this data is not
  // available, MessageReference may fetch what it needs for the chosen display,
  // preview, or open target.
  summary?: MessageSummary | null;
  detail?: MessageDetail | null;
  isLoading?: boolean;
  error?: string | null;

  onOpen?: (messageId: number) => void;
};
```

Defaults:

- `preview`: `compact`
- `openTarget`: `none`
- `display`: no implicit default; callers must choose the reference identity

Invalid prop combinations should fail visibly in development. For example, `display="citation-number"` without a
`citationNumber` should fall back to `display="message-id"` in production but warn in development.

Display values answer one question: **what identity should this reference expose?**

| `display` | Essence | Typical visual |
| --- | --- | --- |
| `citation-number` | Show local citation number | `[1]` |
| `subject` | Show message subject | mail icon + `Atlas prototype review` |
| `message-id` | Show source message ID | mail icon + `Email #2043` |
| `link-text` | Show caller-provided link text | regular hyperlink text |

The visual styling should be derived from the display value by default:

- `citation-number`: compact light-green evidence chip.
- `subject`: neutral subject pill with mail icon.
- `message-id`: neutral source-ID pill with mail icon.
- `link-text`: normal inline link styling.

Missing-message state overrides the normal visual tone with an error treatment while preserving the display family. For
example, `display="subject"` should fall back to `Message #999999999 (not found)`.

`preview` answers one question: **should this reference expose a compact message preview?**

| `preview` | Meaning |
| --- | --- |
| `compact` | Show `MessagePreview layout="compact"` using the best interaction for the device |
| `none` | Do not show an intermediate preview |

Desktop should generally reveal `preview="compact"` on hover and keyboard focus. Touch-first surfaces should reveal the
same compact preview on tap. The preview layout stays the same; only the reveal interaction changes.

Use `preview="none"` when:

1. The reference already sits next to a visible message preview.
2. The UI is a very dense debug/admin list where previews would be noisy.
3. A touch-first flow should open the full panel/drawer directly.
4. The reference is external-link-only.
5. An inline expanded message is already open below the reference.
6. The surface should not fetch just to support previews.

### Message data loading

`MessageReference` should use caller-provided `summary` or `detail` when available. If the caller only has a
`messageId`, `MessageReference` may fetch message data when the chosen configuration requires it.

Fetch data when needed for:

1. `display="subject"`, because the subject is the visible label.
2. `preview="compact"`, because the compact preview needs sender, date, subject, and snippet.
3. `openTarget="external"`, if an external URL is needed.

Do not fetch just to render `display="citation-number"` with `preview="none"`, because the citation number can be drawn
from `messageId` and `citationNumber`.

The fetch behavior should live in a shared hook rather than inside layout code:

```tsx
type MessageDataState = {
  summary?: MessageSummary | null;
  detail?: MessageDetail | null;
  isLoading: boolean;
  error?: { status?: number; message: string } | null;
};

function useMessageReferenceData(
  messageId: number,
  options: {
    enabled: boolean;
    summary?: MessageSummary | null;
    detail?: MessageDetail | null;
  },
): MessageDataState;
```

It should:

1. Fetch `/api/messages/:id` once per ID when enabled.
2. Cache successful results for the current page lifetime.
3. Cache failed attempts for the current page lifetime to avoid repeated 404 loops.
4. Return one shared state shape used by both `MessageReference` and `MessagePreview`.

This replaces the current `MessagePill` fetch responsibility. `MessagePill` should not remain as a long-term wrapper.

Data precedence:

1. Caller-provided `detail` wins over fetched detail.
2. Caller-provided `summary` wins over fetched summary for page-local labels and snippets.
3. Fetched data fills missing fields for ID-only contexts such as Ask Memento.
4. Missing-source state is sticky for the current page lifetime after a 404.

Normalize data before rendering so layouts do not need to know whether their data came from `/api/messages/:id` or a
dimension-page timeline row:

```tsx
type NormalizedMessagePreview = {
  messageId: number;
  subject: string;
  snippet: string;
  bodyText?: string;
  sentAt?: string;
  dateLabel?: string;
  fromLabel?: string;
  fromEmail?: string;
  directionLabel?: string;
  sourceLabel?: string;
  externalUrl?: string;
  sourceType?: string;
};
```

All display layouts must apply the same privacy masking rules currently used by dimension pages before showing names,
email addresses, subjects, snippets, or body text.

### `MessagePreview`

`MessagePreview` renders the message content in a specific layout. It is presentational: callers pass summary/detail
data, loading state, and errors.

```tsx
type MessagePreviewProps = {
  messageId: number;

  layout: "compact" | "row" | "inline-expanded" | "side-panel";

  summary?: MessageSummary | null;
  detail?: MessageDetail | null;
  isLoading?: boolean;
  error?: string | null;

  onOpen?: (messageId: number) => void;
  onLocate?: (messageId: number) => void;
  externalUrl?: string;

  initiallyExpanded?: boolean;
  showActions?: boolean;
};
```

The component should derive data depth from the provided props instead of requiring a separate `dataMode`. `MessagePreview`
does not fetch; callers pass summary/detail/error/loading state directly or via `MessageReference`.

| Layout | Expected data | Current UI equivalent |
| --- | --- | --- |
| `compact` | `summary` preferred, `detail` acceptable | `CitationHoverCard`; AgentChat should adopt this look |
| `row` | `summary` preferred, `detail` acceptable | Supporting Emails / Message Archive rows via `MessageRow` |
| `inline-expanded` | `detail` preferred, fallback to summary | Inline "show more" style for review/group contexts |
| `side-panel` | `detail` preferred, fallback to summary while loading | right-side Supporting email panel via `MessagePreviewPanel` |

Error rendering by layout:

| Layout | Missing message or failed preview behavior |
| --- | --- |
| `compact` | Show `Message #N`, `Source message not found.` for 404, or `Preview unavailable.` for other failures |
| `row` | Render a selectable error row with the message ID and unavailable reason |
| `inline-expanded` | Render an inline error card that does not hide surrounding content |
| `side-panel` | Render the normal panel shell with an error message in the excerpt area |

## 4. Current UI mapping

### Project, person, concept, and newsletter narrative citations

Use `MessageReference`.

```tsx
<MessageReference
  messageId={2112}
  display="citation-number"
  citationNumber={7}
  summary={timelineById.get(2112)}
  preview="compact"
  openTarget="right-panel"
  onOpen={setSelectedMessageId}
/>
```

Behavior:

- Shows `[7]`.
- Desktop hover/focus and mobile tap show `MessagePreview layout="compact"`.
- Click selects the message in the right-side panel.

### Ask Memento source references

Use `MessageReference`. The component can fetch message data because Ask Memento usually only has a raw message ID.

```tsx
<MessageReference
  messageId={2043}
  display="subject"
  preview="compact"
  openTarget="none"
/>
```

Behavior:

- Shows the message subject when loaded.
- Falls back to `Email #2043` while loading or if no subject is available.
- Shows `Message #999999999 (not found)` for missing messages.
- Desktop hover/focus and mobile tap use the shared compact preview look.

### Backfill candidate source references

Use `MessageReference` if the candidate appears as a compact source reference, or `MessagePreview layout="row"` if
the candidate is being shown as a message in a list.

Compact reference:

```tsx
<MessageReference
  messageId={2043}
  display="subject"
  preview="compact"
  openTarget="none"
/>
```

Message row:

```tsx
<MessagePreview
  messageId={2043}
  layout="row"
  summary={candidateSummary}
  onOpen={setSelectedMessageId}
/>
```

### Supporting Emails / Message Archive rows

Use `MessagePreview`, not `MessageReference`.

```tsx
<MessagePreview
  messageId={2109}
  layout="row"
  summary={message}
  onOpen={setSelectedMessageId}
/>
```

These rows present messages associated with a dimension page. They are not citations by themselves.

### Right-side Supporting email panel

Use `MessagePreview`.

```tsx
<MessagePreview
  messageId={2112}
  layout="side-panel"
  summary={selectedMessageSummary}
  detail={selectedMessageDetail}
  isLoading={selectedMessageLoading}
  error={selectedMessageError}
  onLocate={locateMessageInTimeline}
  showActions
/>
```

### Inline expanded preview

Use `MessagePreview`.

```tsx
<MessagePreview
  messageId={2043}
  layout="inline-expanded"
  summary={summary}
  detail={detail}
  isLoading={isLoading}
  error={error}
  showActions
/>
```

This layout is intended for review/group UIs where users need to inspect a message in place without moving to a right
rail.

## 5. Interaction axes

Compact preview and full-message display are separate axes.

`preview` answers: **should this reference show an intermediate compact preview?**

`openTarget` answers: **what happens on click or keyboard activation?**

Examples:

| Context | Display | Preview | Open target |
| --- | --- | --- | --- |
| Ask Memento reference | `subject` | `compact` | `none` initially |
| Project narrative citation | `citation-number` | `compact` | `right-panel` |
| Supporting Emails row | preview row, not reference | not applicable | `right-panel` |
| Review/group inline inspection | reference or row | `compact` or `none` | `inline` |
| Mobile/touch-first UI | any | same `compact` preview, revealed by tap | `right-panel` or `inline` |

`MessageReference` should decide how to reveal `preview="compact"` based on input modality:

- Desktop pointer device: hover and keyboard focus.
- Touch-first device: first tap opens the compact preview.
- Keyboard: focus and/or activation must provide an accessible path to the preview and open target.

If `preview="compact"` and `openTarget` is not `none`, touch-first UIs should not make one tap do two things. Use this
sequence instead:

1. First tap opens the compact preview.
2. The compact preview exposes an Open action.
3. The Open action resolves `openTarget`.

If `preview="none"`, tapping or keyboard activation resolves `openTarget` directly.

Responsive open-target mapping:

- `right-panel`: right rail on desktop, drawer or sheet on narrow screens.
- `inline`: expand in place in both desktop and mobile layouts.
- `external`: open the source URL in a new tab/window.
- `none`: do not open a full preview target; preview behavior may still exist.

Accessibility requirements:

- References must be keyboard focusable.
- Compact previews must dismiss on Escape and when focus leaves the reference/preview group.
- Compact previews must have accessible names such as `Preview Email #2043`.
- Open actions must have accessible names such as `Open Email #2043`.
- Hover-only access is not sufficient.

## 6. Data ownership

Prefer presentational preview layouts plus one shared message-data hook.

Dimension pages usually already have summary data in memory:

- Timeline items
- Message archive rows
- Citation source maps

Those pages should pass summaries directly to `MessageReference` or `MessagePreview`, and use the existing
`useMessageDetail` pattern only for the selected side-panel message.

Ask Memento usually only has raw `[msg:N]` references in text. It should use `MessageReference`, which fetches message
data only when its display, preview, or open target requires it.

## 7. Parsing ownership

Parsing source-message tokens is caller-owned today, but should converge over time.

Current parsing paths:

- Ask Memento markdown rewrites bare `[msg:N]` tokens into internal links before rendering.
- Dimension pages use `EvidenceText` to split text around `[msg:N]` tokens.
- Citation chip lists already receive explicit message IDs.

Target path:

1. Add a shared `MessageReferenceText` helper that parses `[msg:N]` tokens and renders `MessageReference`.
2. Keep citation-number assignment caller-owned, because only the page/section owns local citation ordering.
3. Preserve user-authored Ask Memento messages as literal text unless the caller explicitly opts into rendering
   references.
4. Use hash-link markdown only as an implementation bridge, not as the long-term parsing API.

## 8. Implementation checklist

Track implementation against this checklist and update it as each task lands.

- [x] Define the shared message data contract and adapters for API detail data and dimension-page summary data.
- [x] Add `useMessageReferenceData` with per-page success and failure caching.
- [x] Add `MessagePreview layout="compact"` using the current dimension-page compact-card visual language.
- [x] Add `MessageReference` with `display`, `preview`, and `openTarget` support.
- [x] Replace `MessagePill` in Ask Memento, SidePanel, and BackfillCard.
- [x] Replace dimension-page citation chips with `MessageReference display="citation-number"`.
- [x] Fold `MessageRow` into `MessagePreview layout="row"`.
- [ ] Fold `MessagePreviewPanel` into `MessagePreview layout="side-panel"`.
- [ ] Add `MessagePreview layout="inline-expanded"` where review/group UIs need in-place detail.
- [ ] Delete replaced legacy components after all call sites are migrated.

## 9. Migration plan

Do this incrementally.

1. Create `MessagePreview layout="compact"` using the current dimension-page compact-card look.
2. Add `MessageReference` and shared `useMessageReferenceData`.
3. Replace `MessagePill` call sites in Ask Memento, SidePanel, and BackfillCard.
4. Replace dimension-page `CitationButton` hover rendering with `MessageReference display="citation-number"`.
5. Fold `MessageRow` into `MessagePreview layout="row"`.
6. Fold `MessagePreviewPanel` into `MessagePreview layout="side-panel"`.
7. Add `MessagePreview layout="inline-expanded"` where review/group UIs need in-place detail.
8. Delete replaced components once all call sites are migrated.

Avoid doing every step in one patch. The lowest-risk first slice is replacing `MessagePill` with
`MessageReference` while adopting the shared compact preview look.
