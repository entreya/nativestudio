import { describe, expect, it } from 'vitest';
import { studioThemes, themeVariables } from './themes';

describe('studio themes', () => {
  it('provides the default plus modern brand-inspired variants', () => {
    expect(studioThemes).toHaveLength(20);
    expect(studioThemes[0].id).toBe('default');
    expect(new Set(studioThemes.map(theme => theme.id)).size).toBe(20);
    studioThemes.forEach(theme => {
      expect(theme).toEqual(expect.objectContaining({
        id: expect.any(String),
        name: expect.any(String),
        layout: expect.stringMatching(/^(standard|rounded|floating|glass|compact|terminal|bento|chrome|youtube|netflix|macos|win11|win12|liquidglass)$/),
        bg: expect.stringMatching(/^#/),
        // Glass-style themes (glass, macos, win12, liquidglass) render surface
        // as a translucent rgba() panel by design, not a flat hex color.
        surface: expect.stringMatching(/^(#|rgba\()/),
        text: expect.stringMatching(/^#/),
        accent: expect.stringMatching(/^#/),
      }));
      expect(themeVariables(theme)['--studio-accent']).toBe(theme.accent);
    });
  });
});
