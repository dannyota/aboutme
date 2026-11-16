import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

import { badgeVariants } from '../../app/components/ui/badge';
import { toggleVariants } from '../../app/components/ui/toggle';

const source = (path: string): string =>
  readFileSync(`app/components/ui/${path}`, 'utf8');

describe('primitive token guard', () => {
  it('scrims dialogs and sheets with the overlay token', () => {
    for (const path of [
      'dialog/DialogOverlay.vue',
      'dialog/DialogScrollContent.vue',
      'alert-dialog/AlertDialogContent.vue',
      'sheet/SheetOverlay.vue',
    ]) {
      expect(source(path), path).toContain('bg-overlay');
      expect(source(path), path).not.toMatch(/bg-black\/\d+/u);
    }
  });

  it('lifts dialogs and sheets on the popover with a hairline', () => {
    for (const path of [
      'dialog/DialogContent.vue',
      'dialog/DialogScrollContent.vue',
      'alert-dialog/AlertDialogContent.vue',
      'sheet/SheetContent.vue',
    ]) {
      const file = source(path);
      expect(file, path).toContain('bg-popover text-popover-foreground');
      expect(file, path).toContain('border-hairline');
      expect(file, path).toContain('shadow-[var(--shadow-dialog)]');
    }
    for (const path of [
      'dialog/DialogContent.vue',
      'dialog/DialogScrollContent.vue',
      'alert-dialog/AlertDialogContent.vue',
    ]) {
      expect(source(path), path).toContain('rounded-[var(--radius-dialog)]');
    }
    expect(source('sheet/SheetContent.vue'))
      .toContain('rounded-t-[var(--radius-card)]');
  });

  it('floats menus on the popover with a hairline and 8 px items', () => {
    for (const path of [
      'dropdown-menu/DropdownMenuContent.vue',
      'dropdown-menu/DropdownMenuSubContent.vue',
    ]) {
      const file = source(path);
      expect(file, path).toContain('rounded-[var(--radius-dialog)]');
      expect(file, path).toContain('border-hairline');
      expect(file, path).toContain('shadow-[var(--shadow-float)]');
    }
    for (const path of [
      'dropdown-menu/DropdownMenuItem.vue',
      'dropdown-menu/DropdownMenuCheckboxItem.vue',
      'dropdown-menu/DropdownMenuRadioItem.vue',
      'dropdown-menu/DropdownMenuSubTrigger.vue',
    ]) {
      expect(source(path), path).toContain('rounded-[8px]');
      expect(source(path), path).toContain('focus:bg-accent');
    }
  });

  it('keeps fields on the ground with a control edge and a ring', () => {
    for (const path of [
      'input/Input.vue',
      'native-select/NativeSelect.vue',
      'textarea/Textarea.vue',
    ]) {
      const file = source(path);
      expect(file, path).toContain('bg-background');
      expect(file, path).toContain('border-input');
      expect(file, path).toContain('rounded-md');
      expect(file, path).toContain('focus-visible:ring-ring');
      expect(file, path).toContain('aria-invalid:border-destructive');
      expect(file, path).toContain('selection:bg-coin');
      expect(file, path).not.toContain('selection:bg-primary');
      expect(file, path).not.toContain('shadow-xs');
    }
    for (const path of ['input/Input.vue', 'native-select/NativeSelect.vue']) {
      expect(source(path), path).toContain('h-10 pointer-coarse:h-11');
    }
  });

  it('marks a selected option with an ink pill', () => {
    expect(source('tabs/TabsList.vue')).toContain('rounded-full');
    expect(source('tabs/TabsTrigger.vue')).toContain(
      'data-[state=active]:bg-foreground data-[state=active]:text-background',
    );
    expect(source('tabs/TabsTrigger.vue')).toContain('rounded-full');
    const toggle = toggleVariants();
    expect(toggle).toContain('rounded-full');
    expect(toggle).toContain(
      'data-[state=on]:bg-foreground data-[state=on]:text-background',
    );
    expect(source('toggle-group/ToggleGroupItem.vue'))
      .toContain('first:rounded-l-full');
  });

  it('keeps controls, cards, tooltips and skeletons on the new tokens', () => {
    expect(source('switch/Switch.vue')).toContain(
      'data-[state=checked]:bg-primary data-[state=unchecked]:bg-input',
    );
    expect(source('checkbox/Checkbox.vue'))
      .toContain('data-[state=checked]:bg-primary');
    expect(source('card/Card.vue')).toContain('rounded-[var(--radius-card)]');
    expect(source('card/Card.vue')).not.toContain('shadow-sm');
    expect(source('tooltip/TooltipContent.vue'))
      .toContain('bg-foreground text-background');
    expect(source('tooltip/TooltipContent.vue')).toContain('rounded-[8px]');
    expect(source('skeleton/Skeleton.vue')).toContain('bg-accent');
    expect(badgeVariants()).toContain('rounded-full');
    expect(badgeVariants({ variant: 'destructive' }))
      .toContain('bg-transparent text-destructive');
  });
});
