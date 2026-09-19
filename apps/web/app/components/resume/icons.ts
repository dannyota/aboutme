import {
  Award,
  BadgeCheck,
  BookOpen,
  Bookmark,
  Briefcase,
  Building2,
  Camera,
  ChartLine,
  Code,
  Compass,
  Cpu,
  Database,
  Dumbbell,
  FileText,
  Flag,
  FlaskConical,
  Folder,
  Globe,
  GraduationCap,
  Heart,
  HeartHandshake,
  Landmark,
  Languages,
  Leaf,
  Library,
  Lightbulb,
  Link,
  Mail,
  MapPin,
  Medal,
  Megaphone,
  Mic,
  Microscope,
  Music,
  Newspaper,
  Palette,
  PenTool,
  Phone,
  Plane,
  Presentation,
  Puzzle,
  Rocket,
  Scale,
  School,
  ScrollText,
  Sparkles,
  Star,
  Stethoscope,
  Target,
  Terminal,
  TrendingUp,
  Trophy,
  User,
  Users,
  Wrench,
} from '@lucide/vue';
import type { Section } from '@aboutme/schema';
import type { Component } from 'vue';

import { BrandGitHub, BrandLinkedIn, BrandX } from './brandIcons';

/**
 * The icons the renderer draws. The schema accepts any kebab-case Lucide
 * name; a section heading whose name is not here falls back to its section
 * type's icon (docs/design/templates/contract.md §5.3).
 */
const ICONS: Readonly<Record<string, Component>> = Object.freeze({
  'award': Award,
  'badge-check': BadgeCheck,
  'book-open': BookOpen,
  'bookmark': Bookmark,
  'briefcase': Briefcase,
  'building-2': Building2,
  'camera': Camera,
  'chart-line': ChartLine,
  'code': Code,
  'compass': Compass,
  'cpu': Cpu,
  'database': Database,
  'dumbbell': Dumbbell,
  'file-text': FileText,
  'flag': Flag,
  'flask-conical': FlaskConical,
  'folder': Folder,
  'github': BrandGitHub,
  'globe': Globe,
  'graduation-cap': GraduationCap,
  'heart': Heart,
  'heart-handshake': HeartHandshake,
  'landmark': Landmark,
  'languages': Languages,
  'leaf': Leaf,
  'library': Library,
  'lightbulb': Lightbulb,
  'link': Link,
  'linkedin': BrandLinkedIn,
  'mail': Mail,
  'map-pin': MapPin,
  'medal': Medal,
  'megaphone': Megaphone,
  'mic': Mic,
  'microscope': Microscope,
  'music': Music,
  'newspaper': Newspaper,
  'palette': Palette,
  'pen-tool': PenTool,
  'phone': Phone,
  'plane': Plane,
  'presentation': Presentation,
  'puzzle': Puzzle,
  'rocket': Rocket,
  'scale': Scale,
  'school': School,
  'scroll-text': ScrollText,
  'sparkles': Sparkles,
  'star': Star,
  'stethoscope': Stethoscope,
  'target': Target,
  'terminal': Terminal,
  'trending-up': TrendingUp,
  'trophy': Trophy,
  'twitter': BrandX,
  'user': User,
  'users': Users,
  'wrench': Wrench,
});

export const iconFor = (key: string): Component | null => ICONS[key] ?? null;

export const hasIcon = (key: string): boolean => Object.hasOwn(ICONS, key);

const SECTION_FALLBACK_ICONS: Readonly<
  Record<Section['sectionType'], string>
> = {
  profile: 'user',
  work: 'briefcase',
  education: 'graduation-cap',
  skill: 'code',
  language: 'languages',
  certificate: 'award',
  project: 'folder',
  custom: 'bookmark',
};

/**
 * The icon a section heading draws: its own key when the renderer has it,
 * else its section type's icon. An absent or empty key stays empty.
 */
export function sectionIconKey(section: Section): string | undefined {
  const key = section.iconKey;
  if (key === undefined || key === '' || hasIcon(key)) return key;
  return SECTION_FALLBACK_ICONS[section.sectionType];
}
