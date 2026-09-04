<script setup lang="ts">
import { LogOut, Moon, Settings2, Sun, UserRound } from '@lucide/vue';
import { computed } from 'vue';
import IconButton from '@/components/app/IconButton.vue';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';

defineOptions({ inheritAttrs: false });
const { logout } = useAuth();
const { theme, toggleTheme } = useTheme();
const themeLabel = computed(() =>
  theme.value === 'dark' ? 'Light theme' : 'Dark theme',
);
function signOut(): void {
  void logout();
}
</script>

<template>
  <DropdownMenu>
    <DropdownMenuTrigger as-child>
      <IconButton
        v-bind="$attrs"
        label="Account menu"
        data-testid="account-menu"
      >
        <UserRound aria-hidden="true" />
      </IconButton>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end">
      <DropdownMenuItem
        data-testid="account-menu-settings"
        @select="navigateTo('/app/settings/sessions')"
      >
        <Settings2 aria-hidden="true" />Settings
      </DropdownMenuItem>
      <DropdownMenuItem
        data-testid="theme-toggle"
        @select="toggleTheme"
      >
        <Sun
          v-if="theme === 'dark'"
          aria-hidden="true"
        />
        <Moon
          v-else
          aria-hidden="true"
        />
        {{ themeLabel }}
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
