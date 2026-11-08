/**
 * Preview seed data: the original sample recipes, plan (Mon/Wed/Thu), prep
 * progress and drafts, expanded into full Recipe / Draft objects.
 *
 * `createSeedState()` returns a fresh deep copy every call so callers (and tests)
 * can never mutate shared module state.
 */
import type {
  Aisle,
  Attention,
  CookStep,
  Draft,
  Ingredient,
  Night,
  PlannerState,
  PrepAheadStep,
  PrepTonightStep,
  Recipe,
} from './types'

function ing(
  id: string,
  name: string,
  quantity: number | null,
  unit: string,
  aisle: Aisle,
  isPantry = false,
): Ingredient {
  return { id, name, quantity, unit, aisle, isPantry }
}

function ahead(
  id: string,
  text: string,
  minutes: number,
  keepsDays: number | null,
  storageNote: string,
  label?: string,
): PrepAheadStep {
  return label ? { id, text, label, minutes, keepsDays, storageNote } : { id, text, minutes, keepsDays, storageNote }
}

function tonight(id: string, text: string, minutes: number): PrepTonightStep {
  return { id, text, minutes }
}

function cook(id: string, text: string, timerSeconds: number | null = null): CookStep {
  return { id, text, timerSeconds }
}

interface RecipeInit {
  id: string
  title: string
  shortName?: string
  icon: string
  activeMinutes: number
  totalMinutes: number
  attention: Attention
  tags: string[]
  defaultServings: number
  ingredients: Ingredient[]
  prepAhead: PrepAheadStep[]
  prepTonight: PrepTonightStep[]
  cookTonight: CookStep[]
}

function seedRecipes(): Recipe[] {
  const recipes: RecipeInit[] = [
    {
      id: 'gochujang-chicken',
      title: 'Sheet-pan gochujang chicken & broccoli',
      shortName: 'chicken',
      icon: '🍎',
      activeMinutes: 12,
      totalMinutes: 40,
      attention: 'low',
      tags: ['Low attention', 'Under 15 min'],
      defaultServings: 4,
      ingredients: [
        ing('chicken', 'chicken thighs', 2, 'lb', 'Meat & fish'),
        ing('broccoli', 'broccoli', 2, 'head', 'Produce'),
        ing('onion', 'yellow onions', 1, '', 'Produce'),
        ing('gochujang', 'gochujang', 3, 'tbsp', 'Pantry aisle', true),
        ing('soy', 'soy sauce', 2, 'tbsp', 'Pantry aisle', true),
        ing('oil', 'olive oil', 2, 'tbsp', 'Pantry aisle', true),
      ],
      prepAhead: [
        ahead('dice-onion', 'Dice 1 onion', 3, 3, 'Fridge · 3 days', 'Diced onion'),
        ahead('marinade', 'Mix marinade, bag the chicken', 6, 1, 'Fridge · 1 day', 'Marinade'),
      ],
      prepTonight: [
        tonight('oven', 'Heat the oven to 425°F', 2),
        tonight('chop-broccoli', 'Cut broccoli into florets', 3),
      ],
      cookTonight: [
        cook('roast', 'Spread chicken, onion and broccoli on a sheet pan and roast.', 1380),
        cook('broil', 'Broil for the last 2 minutes until the edges char.', 120),
        cook('serve', 'Serve straight from the pan.'),
      ],
    },
    {
      id: 'bean-sweet-potato-chili',
      title: 'Slow-cooker bean & sweet potato chili',
      shortName: 'chili',
      icon: '🍄',
      activeMinutes: 10,
      totalMinutes: 360,
      attention: 'low',
      tags: ['Dump-and-go', 'Low attention', 'Under 15 min', 'Vegetarian'],
      defaultServings: 5,
      ingredients: [
        ing('sweet-potato', 'sweet potatoes', 2, '', 'Produce'),
        ing('onion', 'yellow onions', 2, '', 'Produce'),
        ing('black-beans', 'black beans', 2, 'can', 'Pantry aisle'),
        ing('crushed-tomatoes', 'crushed tomatoes', 1, 'can', 'Pantry aisle'),
        ing('chili-powder', 'chili powder', 2, 'tbsp', 'Pantry aisle', true),
        ing('cumin', 'ground cumin', 1, 'tbsp', 'Pantry aisle', true),
        ing('salt', 'salt', 1, 'tsp', 'Pantry aisle', true),
        ing('sour-cream', 'sour cream', 1, 'small tub', 'Dairy'),
        ing('cheddar', 'sharp cheddar', 4, 'oz', 'Dairy'),
      ],
      prepAhead: [
        ahead('dice-onions', 'Dice 2 onions', 4, 3, 'Fridge · 3 days', 'Diced onions'),
        ahead('cube-sweet-potatoes', 'Peel & cube 2 sweet potatoes', 8, 3, 'Fridge, covered · 3 days', 'Cubed sweet potato'),
        ahead('measure-spices', 'Measure chili spices + beans', 9, null, 'Wednesday becomes open-and-pour'),
      ],
      prepTonight: [tonight('rinse-beans', 'Drain and rinse the beans', 3)],
      cookTonight: [
        cook('dump', 'Dump everything into the slow cooker. Cook on low for 6 hours.', 21600),
        cook('top', 'Ladle into bowls; top with sour cream and cheddar.'),
      ],
    },
    {
      id: 'salmon-lemon-herb-orzo',
      title: 'Seared salmon, lemon-herb orzo',
      shortName: 'salmon',
      icon: '🐟',
      activeMinutes: 18,
      totalMinutes: 30,
      attention: 'high',
      tags: ['Hands-on'],
      defaultServings: 4,
      ingredients: [
        ing('salmon', 'salmon fillets', 2, 'lb', 'Meat & fish'),
        ing('orzo', 'orzo', 1.25, 'cup', 'Pantry aisle'),
        ing('lemon', 'lemons', 2, '', 'Produce'),
        ing('parsley', 'parsley', 1, 'bunch', 'Produce'),
        ing('dill', 'dill', 0.5, 'bunch', 'Produce'),
        ing('tomatoes', 'cherry tomatoes', 1, 'pint', 'Produce'),
        ing('oil', 'olive oil', 3, 'tbsp', 'Pantry aisle', true),
      ],
      prepAhead: [
        ahead('herb-sauce', 'Blend herb sauce', 5, 4, 'Fridge · up to 4 days', 'Herb sauce'),
        ahead('toast-orzo', 'Toast orzo until golden', 4, null, 'Pantry', 'Toasted orzo'),
      ],
      prepTonight: [
        tonight('halve-tomatoes', 'Halve the tomatoes and zest a lemon', 4),
        tonight('dry-salmon', 'Pat the salmon dry and season it', 2),
      ],
      cookTonight: [
        cook('orzo', 'Add the orzo to the boiling water.', 540),
        cook('sear', 'Sear salmon skin-side down. Don’t move it.', 270),
        cook('flip', 'Flip and cook the other side.', 120),
        cook('toss', 'Drain orzo; toss with herb sauce, lemon and tomatoes.'),
        cook('rest', 'Rest salmon 2 min, then serve over the orzo.'),
      ],
    },
    {
      id: 'red-lentil-coconut-soup',
      title: 'Red lentil coconut soup',
      shortName: 'lentil soup',
      icon: '🥥',
      activeMinutes: 15,
      totalMinutes: 35,
      attention: 'low',
      tags: ['Low attention', 'Under 15 min', 'Vegetarian'],
      defaultServings: 4,
      ingredients: [
        ing('lentils', 'red lentils', 1.5, 'cup', 'Pantry aisle'),
        ing('coconut-milk', 'coconut milk', 2, 'can', 'Pantry aisle'),
        ing('onion', 'yellow onions', 1, '', 'Produce'),
        ing('carrots', 'carrots', 3, '', 'Produce'),
        ing('broth', 'vegetable broth', 4, 'cup', 'Pantry aisle'),
        ing('lime', 'limes', 1, '', 'Produce'),
        ing('curry', 'curry powder', 2, 'tbsp', 'Pantry aisle', true),
        ing('oil', 'olive oil', 1, 'tbsp', 'Pantry aisle', true),
      ],
      prepAhead: [ahead('chop-aromatics', 'Dice the onion and carrots', 6, 3, 'Fridge · 3 days', 'Chopped aromatics')],
      prepTonight: [tonight('rinse-lentils', 'Rinse the lentils', 2)],
      cookTonight: [
        cook('sweat', 'Soften onion and carrots in oil with the curry powder.', 300),
        cook('simmer', 'Add lentils, broth and coconut milk; simmer until the lentils fall apart.', 1200),
        cook('finish', 'Squeeze in lime and serve.'),
      ],
    },
    {
      id: 'gnocchi-peppers',
      title: 'Sheet-pan gnocchi with peppers',
      shortName: 'gnocchi',
      icon: '🫑',
      activeMinutes: 8,
      totalMinutes: 30,
      attention: 'low',
      tags: ['Low attention', 'Under 15 min', 'Vegetarian'],
      defaultServings: 4,
      ingredients: [
        ing('gnocchi', 'shelf-stable gnocchi', 2, 'lb', 'Pantry aisle'),
        ing('peppers', 'bell peppers', 3, '', 'Produce'),
        ing('red-onion', 'red onions', 1, '', 'Produce'),
        ing('mozzarella', 'fresh mozzarella', 4, 'oz', 'Dairy'),
        ing('italian', 'Italian seasoning', 2, 'tsp', 'Pantry aisle', true),
        ing('oil', 'olive oil', 3, 'tbsp', 'Pantry aisle', true),
      ],
      prepAhead: [ahead('slice-veg', 'Slice peppers and onion', 6, 3, 'Fridge · 3 days', 'Sliced peppers')],
      prepTonight: [tonight('oven', 'Heat the oven to 450°F', 2)],
      cookTonight: [
        cook('toss', 'Toss gnocchi and vegetables with oil and seasoning on a sheet pan.'),
        cook('roast', 'Roast until the gnocchi are golden and crisp.', 1200),
        cook('cheese', 'Scatter mozzarella on top and roast 2 minutes more.', 120),
      ],
    },
    {
      id: 'pork-kimchi-fried-rice',
      title: 'Pork & kimchi fried rice',
      shortName: 'fried rice',
      icon: '🥢',
      activeMinutes: 14,
      totalMinutes: 20,
      attention: 'medium',
      tags: ['Under 15 min'],
      defaultServings: 4,
      ingredients: [
        ing('pork', 'ground pork', 1, 'lb', 'Meat & fish'),
        ing('kimchi', 'kimchi', 1, 'cup', 'Produce'),
        ing('rice', 'cooked rice', 4, 'cup', 'Pantry aisle'),
        ing('eggs', 'eggs', 4, '', 'Dairy'),
        ing('scallions', 'scallions', 1, 'bunch', 'Produce'),
        ing('peas', 'frozen peas', 1, 'cup', 'Frozen'),
        ing('soy', 'soy sauce', 2, 'tbsp', 'Pantry aisle', true),
        ing('sesame', 'sesame oil', 1, 'tbsp', 'Pantry aisle', true),
      ],
      prepAhead: [ahead('cook-rice', 'Cook the rice and chill it', 5, 3, 'Fridge, spread flat · 3 days', 'Cooked rice')],
      prepTonight: [tonight('slice-scallions', 'Slice the scallions', 3)],
      cookTonight: [
        cook('brown', 'Brown the pork in a hot wok.', 300),
        cook('kimchi', 'Add kimchi and cook until it sizzles.', 120),
        cook('rice', 'Add rice, peas and soy sauce; fry until hot.', 240),
        cook('egg', 'Push the rice aside, scramble the eggs, then fold together.'),
      ],
    },
    {
      id: 'tortilla-soup',
      title: 'Slow-cooker chicken tortilla soup',
      shortName: 'tortilla soup',
      icon: '🥣',
      activeMinutes: 9,
      totalMinutes: 360,
      attention: 'low',
      tags: ['Dump-and-go', 'Low attention', 'Under 15 min'],
      defaultServings: 4,
      ingredients: [
        ing('chicken', 'chicken breasts', 1.5, 'lb', 'Meat & fish'),
        ing('onion', 'yellow onions', 1, '', 'Produce'),
        ing('black-beans', 'black beans', 1, 'can', 'Pantry aisle'),
        ing('crushed-tomatoes', 'crushed tomatoes', 1, 'can', 'Pantry aisle'),
        ing('broth', 'chicken broth', 4, 'cup', 'Pantry aisle'),
        ing('corn', 'frozen corn', 1, 'cup', 'Frozen'),
        ing('lime', 'limes', 2, '', 'Produce'),
        ing('avocado', 'avocados', 2, '', 'Produce'),
        ing('chips', 'tortilla chips', 1, 'bag', 'Pantry aisle'),
        ing('cumin', 'ground cumin', 1, 'tbsp', 'Pantry aisle', true),
      ],
      prepAhead: [ahead('dice-onion', 'Dice the onion', 3, 3, 'Fridge · 3 days', 'Diced onion')],
      prepTonight: [tonight('drain-beans', 'Drain and rinse the beans', 2)],
      cookTonight: [
        cook('dump', 'Put chicken, onion, beans, tomatoes, broth and cumin in the slow cooker. Cook on low for 6 hours.', 21600),
        cook('shred', 'Shred the chicken and stir it back in.', 300),
        cook('serve', 'Serve topped with chips, avocado and lime.'),
      ],
    },
    {
      id: 'mushroom-leek-risotto',
      title: 'Mushroom & leek risotto',
      shortName: 'risotto',
      icon: '🍚',
      activeMinutes: 30,
      totalMinutes: 40,
      attention: 'high',
      tags: ['Hands-on', 'Vegetarian'],
      defaultServings: 4,
      ingredients: [
        ing('rice', 'arborio rice', 1.5, 'cup', 'Pantry aisle'),
        ing('leeks', 'leeks', 2, '', 'Produce'),
        ing('mushrooms', 'cremini mushrooms', 12, 'oz', 'Produce'),
        ing('broth', 'vegetable broth', 5, 'cup', 'Pantry aisle'),
        ing('parmesan', 'parmesan', 3, 'oz', 'Dairy'),
        ing('wine', 'dry white wine', 0.5, 'cup', 'Pantry aisle'),
        ing('butter', 'butter', 2, 'tbsp', 'Dairy', true),
        ing('oil', 'olive oil', 1, 'tbsp', 'Pantry aisle', true),
      ],
      prepAhead: [
        ahead('clean-leeks', 'Wash and slice the leeks', 6, 2, 'Fridge · 2 days', 'Sliced leeks'),
        ahead('slice-mushrooms', 'Slice the mushrooms', 5, 2, 'Fridge, paper towel · 2 days', 'Sliced mushrooms'),
      ],
      prepTonight: [tonight('warm-broth', 'Warm the broth in a small pot', 3)],
      cookTonight: [
        cook('saute', 'Sauté mushrooms until browned, then the leeks until soft.', 480),
        cook('toast', 'Stir in the rice for a minute, then the wine until absorbed.', 120),
        cook('ladle', 'Add broth a ladle at a time, stirring, until the rice is creamy.', 1080),
        cook('finish', 'Stir in butter and parmesan. Serve right away.'),
      ],
    },
  ]
  return recipes
}

function seedDrafts(): Draft[] {
  return [
    {
      id: 'draft-black-bean-chili',
      revision: 3,
      title: 'Slow-cooker black bean & sweet potato chili',
      icon: '🍲',
      activeMinutes: 10,
      totalMinutes: 360,
      attention: 'low',
      tags: ['Dump-and-go', 'Vegetarian'],
      defaultServings: 5,
      ingredients: [
        ing('sweet-potato', 'sweet potatoes', 2, '', 'Produce'),
        ing('onion', 'yellow onions', 2, '', 'Produce'),
        ing('black-beans', 'black beans', 2, 'can', 'Pantry aisle'),
        ing('crushed-tomatoes', 'crushed tomatoes', 1, 'can', 'Pantry aisle'),
        ing('chili-powder', 'chili powder', 2, 'tbsp', 'Pantry aisle', true),
      ],
      prepAhead: [ahead('dice-veg', 'Dice onion + sweet potato the day before', 12, 3, 'Diced sweet potato keeps 3 days, fridge')],
      prepTonight: [tonight('rinse', 'Drain and rinse the beans', 3)],
      cookTonight: [cook('dump', 'Add everything to the slow cooker; cook on low for 6 hours.', 21600)],
      warnings: ['Total time is a guess from the source notes.', 'Storage claim has no source.'],
      confirmed: { activeMinutes: false, totalMinutes: true, attention: false, prepAhead: false, storage: false },
    },
    {
      id: 'draft-fennel-traybake',
      revision: 1,
      title: 'Chicken thigh traybake with fennel',
      icon: '🍗',
      activeMinutes: 15,
      totalMinutes: 55,
      attention: 'medium',
      tags: ['Low attention'],
      defaultServings: 4,
      ingredients: [
        ing('chicken', 'chicken thighs', 2, 'lb', 'Meat & fish'),
        ing('fennel', 'fennel bulbs', 2, '', 'Produce'),
        ing('potatoes', 'baby potatoes', 1.5, 'lb', 'Produce'),
        ing('oil', 'olive oil', 3, 'tbsp', 'Pantry aisle', true),
      ],
      prepAhead: [ahead('slice-fennel', 'Slice the fennel and halve the potatoes', 8, 2, 'Fridge · 2 days')],
      prepTonight: [tonight('oven', 'Heat the oven to 425°F', 2)],
      cookTonight: [cook('roast', 'Roast chicken and vegetables together until the skin is crisp.', 2400)],
      warnings: [],
      confirmed: { activeMinutes: false, totalMinutes: false, attention: false, prepAhead: false, storage: false },
    },
  ]
}

/** Default nights for Plan my week (Mon, Wed, Thu). */
export const DEFAULT_PLAN_DAYS = [1, 3, 4] as const

export function createSeedState(): PlannerState {
  const nights: Night[] = [0, 1, 2, 3, 4, 5, 6].map((day) => ({ day: day as Night['day'], status: 'open', pinned: false }))
  nights[1] = { day: 1, status: 'planned', recipeId: 'gochujang-chicken', servings: 4, pinned: false }
  nights[3] = { day: 3, status: 'planned', recipeId: 'bean-sweet-potato-chili', servings: 5, pinned: false }
  nights[4] = { day: 4, status: 'planned', recipeId: 'salmon-lemon-herb-orzo', servings: 4, pinned: false }

  return {
    recipes: seedRecipes(),
    drafts: seedDrafts(),
    plan: { nights, revision: 0 },
    progress: {
      shoppingChecked: {},
      prepDone: {
        'bean-sweet-potato-chili:dice-onions': {
          doneFactor: 1,
          text: 'Dice 2 onions',
          minutes: 4,
          mealTitle: 'Slow-cooker bean & sweet potato chili',
        },
        'bean-sweet-potato-chili:cube-sweet-potatoes': {
          doneFactor: 1,
          text: 'Peel & cube 2 sweet potatoes',
          minutes: 8,
          mealTitle: 'Slow-cooker bean & sweet potato chili',
        },
      },
    },
    pantryLow: ['olive oil'],
    houseRules: { targetDinners: 3, prepDay: 0 },
    proposal: null,
    undo: null,
    status: null,
    seq: 0,
  }
}
