<script setup lang="ts">
import { LogOut, Settings2, UserRound } from '@lucide/vue';
import { computed } from 'vue';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

defineOptions({ inheritAttrs: false });
const { user, logout } = useAuth();
const accountName = computed(
  () => user.value?.name?.trim() || user.value?.email || 'Account',
);
const initials = computed(() =>
  accountName.value
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? '')
    .join(''),
);
function signOut(): void {
  void logout();
}
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <Button
        v-bind="$attrs"
        :aria-label="`Account settings for ${accountName}`"
        :class="cn('max-w-56 gap-2 pl-1', $attrs.class)"
        data-testid="account-menu"
        size="sm"
        variant="ghost"
      >
        <Avatar class="size-7">
          <AvatarFallback class="bg-secondary text-xs text-foreground">
            <UserRound
              v-if="!user?.name && !user?.email"
              aria-hidden="true"
              class="size-4"
            />
            <template v-else>
              {{ initials }}
            </template>
          </AvatarFallback>
        </Avatar>
        <span class="truncate">{{ accountName }}</span>
      </Button>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end">
      <DropdownMenuItem
        data-testid="account-menu-settings"
        @select="navigateTo('/app/settings/sessions')"
      >
        <Settings2 aria-hidden="true" />Settings
      </DropdownMenuItem>
      <DropdownMenuSeparator />
      <DropdownMenuItem
        data-testid="account-menu-logout"
        @select="signOut"
      >
        <LogOut aria-hidden="true" />Log out
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
