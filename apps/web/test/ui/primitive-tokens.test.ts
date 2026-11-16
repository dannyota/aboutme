import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

import { badgeVariants } from '../../app/components/ui/badge';
import { toggleVariants } from '../../app/components/ui/toggle';

const source = (path: string): string =>
  readFileSync(`app/components/ui/${path}`, 'utf8');

type Guard = {
  readonly has: readonly string[];
  readonly lacks?: readonly string[];
};

const FIELD_LACKS = [
  'selection:bg-primary', 'shadow-xs', 'dark:bg-input/30',
  'focus-visible:ring-ring/50', 'focus-visible:ring-3',
  'aria-invalid:ring-destructive/20', 'md:text-sm', 'bg-transparent px-3',
];
const FOCUS_RING = 'focus-visible:ring-2 focus-visible:ring-ring '
  + 'focus-visible:ring-offset-2 focus-visible:ring-offset-background';
const DIALOG_HAS = [
  'bg-popover text-popover-foreground', 'border-hairline',
  'shadow-[var(--shadow-dialog)]',
];
const DIALOG_LACKS = [
  'bg-black/80', 'shadow-lg', 'rounded-lg', '\'bg-background',
];
const MENU_ITEM: Guard = {
  has: ['rounded-[8px]', 'focus:bg-accent'],
  lacks: ['rounded-sm'],
};

const GUARDS: Record<string, Guard> = {
  'input/Input.vue': {
    has: [
      'selection:bg-coin selection:text-coin-ink', 'border-input',
      'h-10 pointer-coarse:h-11', 'rounded-md', 'bg-background',
      'text-base font-medium',
      'focus-visible:border-ring focus-visible:ring-1 focus-visible:ring-ring',
      '\'aria-invalid:border-destructive\'',
    ],
    lacks: FIELD_LACKS,
  },
  'native-select/NativeSelect.vue': {
    has: [
      'selection:bg-coin selection:text-coin-ink', 'border-input',
      'h-10 pointer-coarse:h-11', 'rounded-md', 'bg-background',
      'text-base font-medium',
      'focus-visible:border-ring focus-visible:ring-1 focus-visible:ring-ring',
      '\'aria-invalid:border-destructive\'',
    ],
    lacks: [...FIELD_LACKS, 'dark:hover:bg-input/50', 'text-sm'],
  },
  'textarea/Textarea.vue': {
    has: [
      'selection:bg-coin selection:text-coin-ink', 'border-input',
      'rounded-md', 'bg-background', 'text-base font-medium',
      'focus-visible:border-ring focus-visible:ring-1 focus-visible:ring-ring',
      'aria-invalid:border-destructive',
    ],
    lacks: FIELD_LACKS,
  },
  'switch/Switch.vue': {
    has: [
      'data-[state=checked]:bg-primary data-[state=unchecked]:bg-input',
      FOCUS_RING, 'rounded-full',
    ],
    lacks: [
      'focus-visible:border-ring', 'focus-visible:ring-ring/50',
      'focus-visible:ring-3', 'dark:data-[state=unchecked]:bg-input/80',
      'shadow-xs',
    ],
  },
  'checkbox/Checkbox.vue': {
    has: [
      'data-[state=checked]:bg-primary', FOCUS_RING,
      'aria-invalid:border-destructive', 'border bg-background',
    ],
    lacks: [
      'focus-visible:border-ring', 'focus-visible:ring-ring/50',
      'focus-visible:ring-3', 'aria-invalid:ring-destructive/20',
      'shadow-xs',
    ],
  },
  'tabs/TabsList.vue': {
    has: ['bg-muted', 'rounded-full'],
    lacks: ['rounded-lg'],
  },
  'tabs/TabsTrigger.vue': {
    has: [
      'data-[state=active]:bg-foreground data-[state=active]:text-background',
      'focus-visible:ring-2 focus-visible:ring-ring',
      'text-muted-foreground hover:text-foreground', 'rounded-full',
      'px-3', 'font-semibold',
      'transition-[color,background-color,box-shadow] outline-none',
    ],
    lacks: [
      'data-[state=active]:bg-background', 'data-[state=active]:shadow-sm',
      'dark:data-[state=active]', 'focus-visible:ring-3', 'rounded-md',
      'font-medium',
    ],
  },
  'toggle-group/ToggleGroup.vue': {
    has: ['items-center gap-[--spacing(var(--gap))] rounded-full\''],
    lacks: ['rounded-md', 'shadow-xs'],
  },
  'toggle-group/ToggleGroupItem.vue': {
    has: [
      'data-[spacing=0]:first:rounded-l-full',
      'data-[spacing=0]:last:rounded-r-full',
    ],
    lacks: ['rounded-l-md', 'rounded-r-md'],
  },
  'dialog/DialogContent.vue': {
    has: [...DIALOG_HAS, 'rounded-[var(--radius-dialog)]',
      'absolute top-4 right-4 rounded-full'],
    lacks: [...DIALOG_LACKS, 'rounded-xs'],
  },
  'dialog/DialogOverlay.vue': { has: ['bg-overlay'], lacks: ['bg-black/80'] },
  'dialog/DialogScrollContent.vue': {
    has: [...DIALOG_HAS, 'bg-overlay',
      'sm:rounded-[var(--radius-dialog)]',
      'rounded-full hover:bg-accent'],
    lacks: [...DIALOG_LACKS, 'border-border', 'hover:bg-secondary'],
  },
  'alert-dialog/AlertDialogContent.vue': {
    has: [...DIALOG_HAS, 'bg-overlay', 'rounded-[var(--radius-dialog)]'],
    lacks: DIALOG_LACKS,
  },
  'sheet/SheetOverlay.vue': { has: ['bg-overlay'], lacks: ['bg-black/80'] },
  'sheet/SheetContent.vue': {
    has: [...DIALOG_HAS, 'rounded-t-[var(--radius-card)]',
      'absolute top-4 right-4 rounded-full'],
    lacks: [...DIALOG_LACKS, 'rounded-xs'],
  },
  'dropdown-menu/DropdownMenuContent.vue': {
    has: ['bg-popover', 'rounded-[var(--radius-dialog)]', 'border-hairline',
      'shadow-[var(--shadow-float)]'],
    lacks: ['rounded-md', 'shadow-md'],
  },
  'dropdown-menu/DropdownMenuSubContent.vue': {
    has: ['bg-popover', 'rounded-[var(--radius-dialog)]', 'border-hairline',
      'shadow-[var(--shadow-float)]'],
    lacks: ['rounded-md', 'shadow-lg'],
  },
  'dropdown-menu/DropdownMenuItem.vue': MENU_ITEM,
  'dropdown-menu/DropdownMenuCheckboxItem.vue': MENU_ITEM,
  'dropdown-menu/DropdownMenuRadioItem.vue': MENU_ITEM,
  'dropdown-menu/DropdownMenuSubTrigger.vue': MENU_ITEM,
  'tooltip/TooltipContent.vue': {
    has: ['bg-foreground text-background', 'rounded-[8px]'],
    lacks: ['rounded-md'],
  },
  'skeleton/Skeleton.vue': {
    has: ['bg-accent'],
    lacks: ['bg-primary/10'],
  },
  'card/Card.vue': {
    has: ['rounded-[var(--radius-card)]', 'border py-6\''],
    lacks: ['rounded-xl', 'shadow-sm'],
  },
};

describe('primitive token guard', () => {
  for (const [path, guard] of Object.entries(GUARDS)) {
    it(`keeps the local token edits in ${path}`, () => {
      const file = source(path);
      for (const name of guard.has) {
        expect(file, `${path} has ${name}`).toContain(name);
      }
      for (const name of guard.lacks ?? []) {
        expect(file, `${path} lacks ${name}`).not.toContain(name);
      }
    });
  }

  it('keeps the local token edits in the toggle variants', () => {
    const toggle = toggleVariants();
    for (const name of [
      'rounded-full', 'font-semibold', 'hover:bg-accent',
      'hover:text-accent-foreground',
      'data-[state=on]:bg-foreground data-[state=on]:text-background',
      'focus-visible:ring-2 focus-visible:ring-ring',
      'transition-[color,background-color,box-shadow]',
      'aria-invalid:border-destructive',
    ]) {
      expect(toggle, name).toContain(name);
    }
    for (const name of [
      'rounded-md', 'font-medium', 'hover:bg-muted',
      'data-[state=on]:bg-accent',
      'focus-visible:ring-ring/50', 'focus-visible:ring-3',
      'aria-invalid:ring-destructive/20',
    ]) {
      expect(toggle, name).not.toContain(name);
    }
    const outline = toggleVariants({ variant: 'outline' });
    expect(outline).toContain('border border-input bg-transparent');
    expect(outline).not.toContain('shadow-xs');
  });

  it('keeps the local token edits in the badge variants', () => {
    expect(badgeVariants()).toContain('rounded-full');
    expect(badgeVariants()).toContain('[a&]:hover:bg-primary-hover');
    expect(badgeVariants()).not.toContain('bg-primary/90');
    const destructive = badgeVariants({ variant: 'destructive' });
    expect(destructive).toContain(
      'border-destructive bg-transparent text-destructive '
      + '[a&]:hover:bg-destructive/10',
    );
    expect(destructive).not.toContain('bg-destructive ');
    expect(destructive).not.toContain('text-white');
  });
});
