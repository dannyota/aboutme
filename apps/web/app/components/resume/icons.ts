import {
  Award,
  Briefcase,
  Code,
  Folder,
  Globe,
  GraduationCap,
  Languages,
  Link,
  Mail,
  MapPin,
  Phone,
  Trophy,
  User,
} from '@lucide/vue';
import type { Component } from 'vue';

import { BrandGitHub, BrandLinkedIn, BrandX } from './brandIcons';

const ICONS: Readonly<Record<string, Component>> = Object.freeze({
  'award': Award,
  'briefcase': Briefcase,
  'code': Code,
  'folder': Folder,
  'github': BrandGitHub,
  'globe': Globe,
  'graduation-cap': GraduationCap,
  'languages': Languages,
  'link': Link,
  'linkedin': BrandLinkedIn,
  'mail': Mail,
  'map-pin': MapPin,
  'phone': Phone,
  'trophy': Trophy,
  'twitter': BrandX,
  'user': User,
});

export const iconFor = (key: string): Component | null => ICONS[key] ?? null;
