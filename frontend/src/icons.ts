// icons.ts — central registry of UI icons.
//
// We render Lucide SVGs inline (no font, no sprite, no extra request) so
// they inherit text color via `currentColor` and pick up size from CSS.
// Each entry is a raw SVG string, imported via Vite's `?raw` suffix from
// the lucide-static package. Tree-shaking keeps only the icons that are
// actually referenced from a view module in the final bundle.

import home from 'lucide-static/icons/house.svg?raw';
import menu from 'lucide-static/icons/menu.svg?raw';
import settings from 'lucide-static/icons/settings.svg?raw';
import messageSquare from 'lucide-static/icons/message-square.svg?raw';
import calendar from 'lucide-static/icons/calendar.svg?raw';
import mail from 'lucide-static/icons/mail.svg?raw';
import brain from 'lucide-static/icons/brain.svg?raw';
import history from 'lucide-static/icons/history.svg?raw';
import sparkles from 'lucide-static/icons/sparkles.svg?raw';
import bookmarkCheck from 'lucide-static/icons/bookmark-check.svg?raw';
import archive from 'lucide-static/icons/archive.svg?raw';
import trash from 'lucide-static/icons/trash-2.svg?raw';
import rotate from 'lucide-static/icons/rotate-cw.svg?raw';
import rocket from 'lucide-static/icons/rocket.svg?raw';
import search from 'lucide-static/icons/search.svg?raw';
import paperclip from 'lucide-static/icons/paperclip.svg?raw';
import loaderCircle from 'lucide-static/icons/loader-circle.svg?raw';
import arrowLeft from 'lucide-static/icons/arrow-left.svg?raw';
import chevronRight from 'lucide-static/icons/chevron-right.svg?raw';

export type IconName =
  | 'home'
  | 'menu'
  | 'settings'
  | 'chat'
  | 'calendar'
  | 'mail'
  | 'memory'
  | 'sessions'
  | 'analyze'
  | 'read'
  | 'archive'
  | 'trash'
  | 'refresh'
  | 'model'
  | 'search'
  | 'paperclip'
  | 'spinner'
  | 'back'
  | 'chevronRight';

const REGISTRY: Record<IconName, string> = {
  home,
  menu,
  settings,
  chat: messageSquare,
  calendar,
  mail,
  memory: brain,
  sessions: history,
  analyze: sparkles,
  read: bookmarkCheck,
  archive,
  trash,
  refresh: rotate,
  model: rocket,
  search,
  paperclip,
  spinner: loaderCircle,
  back: arrowLeft,
  chevronRight,
};

// icon() returns the SVG markup for `name`, with the `lucide-icon` class
// added so CSS can size/color all of them at once. Optional extra class
// lets the caller scope styles (e.g., `icon-on-tile` to flip stroke to
// white when the icon sits inside a colored tile).
export function icon(name: IconName, extraClass?: string): string {
  const raw = REGISTRY[name];
  const cls = extraClass ? `lucide-icon ${extraClass}` : 'lucide-icon';
  // lucide-static SVGs already carry their own `class="lucide lucide-..."`
  // attribute. Replace only the first occurrence; preserves the SVG's
  // semantic class while letting us hook our own styling on top.
  return raw.replace(/class="[^"]*"/, `class="${cls}"`);
}
