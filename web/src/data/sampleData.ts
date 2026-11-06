export type Recipe = {
  title: string
  day?: string
  icon: string
  activeMinutes: number
  total: string
  tags: string[]
  serves?: string
}

export const recipes: Recipe[] = [
  { title: 'Sheet-pan gochujang chicken & broccoli', day: 'Mon', icon: '🍎', activeMinutes: 12, total: '40 min total', tags: ['Low attention', 'Under 15 min'], serves: '4' },
  { title: 'Slow-cooker bean & sweet potato chili', day: 'Wed', icon: '🍄', activeMinutes: 10, total: '6 hrs total', tags: ['Dump-and-go', 'Low attention', 'Under 15 min', 'Vegetarian'], serves: '5 (+1)' },
  { title: 'Seared salmon, lemon-herb orzo', day: 'Thu', icon: '🐟', activeMinutes: 18, total: '30 min total', tags: ['Hands-on'], serves: '4' },
  { title: 'Red lentil coconut soup', icon: '🥥', activeMinutes: 15, total: '35 min total', tags: ['Low attention', 'Under 15 min', 'Vegetarian'] },
  { title: 'Sheet-pan gnocchi with peppers', icon: '🫑', activeMinutes: 8, total: '30 min total', tags: ['Low attention', 'Under 15 min', 'Vegetarian'] },
  { title: 'Pork & kimchi fried rice', icon: '🥢', activeMinutes: 14, total: '20 min total', tags: ['Under 15 min'] },
  { title: 'Slow-cooker chicken tortilla soup', icon: '🥣', activeMinutes: 9, total: '6 hrs total', tags: ['Dump-and-go', 'Low attention', 'Under 15 min'] },
  { title: 'Mushroom & leek risotto', icon: '🍚', activeMinutes: 30, total: '40 min total', tags: ['Hands-on', 'Vegetarian'] },
]

export const prepTasks = [
  { title: 'Dice 3 onions', note: '2 for chili, 1 for the marinade', meal: 'Wed · Chili  •  Mon · Chicken', minutes: 6, done: true },
  { title: 'Peel & cube 2 sweet potatoes', note: '', meal: 'Wed · Chili', minutes: 8, done: true },
  { title: 'Mix marinade, bag the chicken', note: 'Fridge · 1 day', meal: 'Mon · Chicken', minutes: 6, done: false },
  { title: 'Blend herb sauce', note: 'Fridge · up to 4 days', meal: 'Thu · Salmon', minutes: 5, done: false },
  { title: 'Toast orzo until golden', note: 'Pantry', meal: 'Thu · Salmon', minutes: 4, done: false },
  { title: 'Measure chili spices + beans', note: 'Wednesday becomes open-and-pour', meal: 'Wed · Chili', minutes: 9, done: false },
]

export type ShoppingGroup = { name: string; items: { name: string; amount: string; meals?: string }[] }

export const shoppingGroups: ShoppingGroup[] = [
  { name: 'Produce', items: [
    { name: 'sweet potatoes', amount: '2' },
    { name: 'yellow onions', amount: '3', meals: 'chili + chicken' },
    { name: 'broccoli', amount: '2 heads' },
    { name: 'lemons', amount: '2' },
    { name: 'parsley', amount: '1 bunch' },
    { name: 'dill', amount: '½ bunch' },
    { name: 'cherry tomatoes', amount: '1 pint' },
  ] },
  { name: 'Meat & fish', items: [
    { name: 'chicken thighs', amount: '2 lb' },
    { name: 'salmon fillets', amount: '2 lb' },
  ] },
  { name: 'Pantry aisle', items: [
    { name: 'black beans', amount: '2 cans' },
    { name: 'crushed tomatoes', amount: '1 can' },
  ] },
  { name: 'Dairy', items: [
    { name: 'sour cream', amount: '1 small tub' },
    { name: 'sharp cheddar', amount: '4 oz' },
  ] },
]

export const cookSteps = [
  { title: 'Add the orzo to the boiling water.', timer: '9:00', done: true },
  { title: 'Sear salmon skin-side down. Don’t move it.', timer: '4:30', done: false },
  { title: 'Flip and cook the other side.', timer: '2:00', done: false },
  { title: 'Drain orzo; toss with herb sauce, lemon and tomatoes.', done: false },
  { title: 'Rest salmon 2 min, then serve over the orzo.', done: false },
]

export const cookIngredients = [
  ['salmon fillets', '2 lb'],
  ['orzo, toasted Sun', '1¼ cup'],
  ['herb sauce, made Sun', '1 jar'],
  ['lemons', '2'],
  ['cherry tomatoes', '1 pint'],
]
