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
  MessageCircle,
  Phone,
  Trophy,
  User,
} from '@lucide/vue';
import type { Component } from 'vue';

const ICONS: Readonly<Record<string, Component>> = Object.freeze({
  'award': Award,
  'briefcase': Briefcase,
  'code': Code,
  'folder': Folder,
  'github': Code,
  'globe': Globe,
  'graduation-cap': GraduationCap,
  'languages': Languages,
  'linkedin': Link,
  'mail': Mail,
  'map-pin': MapPin,
  'phone': Phone,
  'trophy': Trophy,
  'twitter': MessageCircle,
  'user': User,
});

export const iconFor = (key: string): Component | null => ICONS[key] ?? null;
