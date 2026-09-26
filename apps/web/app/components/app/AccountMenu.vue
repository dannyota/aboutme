<script setup lang="ts">
import {
  ChartColumn, LogOut, Moon, Settings2, Sun, UserRound,
} from '@lucide/vue';
import { computed } from 'vue';
import IconButton from '@/components/app/IconButton.vue';
import { shellCopy } from '@/i18n/shell';
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
const locale = useRouteLocale();
const copy = computed(() => shellCopy[locale.value]);
const themeLabel = computed(() =>
  theme.value === 'dark' ? copy.value.lightTheme : copy.value.darkTheme,
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
        :label="copy.accountMenu"
        data-testid="account-menu"
      >
        <UserRound aria-hidden="true" />
      </IconButton>
    </DropdownMenuTrigger>
    <DropdownMenuContent align="end">
      <DropdownMenuItem
        data-testid="account-menu-views"
        @select="navigateTo('/app/views')"
      >
        <ChartColumn aria-hidden="true" />{{ copy.views }}
      </DropdownMenuItem>
      <DropdownMenuItem
        data-testid="account-menu-settings"
        @select="navigateTo('/app/settings/sessions')"
      >
        <Settings2 aria-hidden="true" />{{ copy.settings }}
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
        <LogOut aria-hidden="true" />{{ copy.logout }}
      </DropdownMenuItem>
    </DropdownMenuContent>
  </DropdownMenu>
</template>
