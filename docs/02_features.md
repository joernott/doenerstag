# Features

The doenerstag app has the following features:

## Data structures

All data structures use UUIDv7 as primary keys in the database and for referencing Items.

### Order

The order is the primary element that is managed by the doenerstag application. An order contains the following elements:

- Masndatory: ID of the creator of the food order
- Mandatory: A restaurant
- Mandatory: Choice between delivery and pickup
- Mandatory: A pickup/delivery time
- Mandatory: A time until when items can be added to the list
- A list of food order items with at least one element. Each entry consists of order items

#### Order items

- Mandatory: ID of the user for the person ordering the item
- Mandatory: A menu item from the restaurant specidfied in the order
- Mandatory: A quantitiy for that item
- Optional: A freetext field for modifications
- Optional: A list of standard modifications specified for the selected menu item

### Restaurants

Restaurants are the starting point for creating an order. Every order can only have one restaurant to order from.

- Mandatory: Name of the restaurant
- Optional: Logo of the restaurant
- Optional: Location/Address for picking up orders
- Mandatory: At least one way of contacting the restaurant, e.g. phone, fax or email

### Opening hours

Opening hours are linked to a restaurant. Every entry in that table contains a time period, during which the restaurant accepts orders. Apart from it's ID and a link to the restaurant. it has the following mandatory fields

- Day of Week
- Start time
- End time

### Menu

Menus are tied to a restaurant. A menu can have categories by which the menu items are ordered.

#### Categories

A Category has the following data fields

- Mandatory: Name
- Optional: Order, a numerical field determining the order of categories

#### Menu item

A menu item has the following elements:

- Mandatory: Name
- Optional: A restaurant specific alphanumeric item id
- Optional: Image
- Optional: Description
- Mandatory: Price
- Mandatory: Currency
- Optional: Tags like "vegan", "vegetarian", "spicy"
- Optional: Tags for allergens and food additives

### Other

#### Version

A table is used to track the application version with the following mandatory fields

- Major
- Minor
- Patch

These are referencing the application version following the concept of semantic versioning.

#### User

A user object has the following fields

- Mandatory: Name
- Mandatory: Password hash
- Optional: EMail address

## User interface

The user interface has a fixed title bar containing the application logo and name as well as a field for the user name/handle, a switch between dark and bright mode and a language selection. A dropdown menu at the right allows for some more menu entries to manage orders, restaurants and their menus as well showing a version page, the imprint and, if not disabled a link to the swagger UI.

By default, the main page shows an overview of existing orders.

## Overview of orders

This page has tiles for orders. Past orders that are expired but not yet removed are faded out but can still be clicked. The first tile shows a plus sign that allows adding new orders. Clicking on the logo in the title will always return to this page.

When clicking on an order, a page is shown for that order.

## Order page

The page is split in two, on the left side, the order data is shown and the list of order items. The user who created ihe order can edit the data fields or delete the whole order. After the first order items have been selected, the restaurant can't be changed any more.

The right side of the page shows the restaurants menu ordered by categories. It can be filtered by tags and allergens or food additives and categories can be hidden or shown. Everybody can add  food order items by choosing menu iterms from the restaurants menu and then change the quantity or add modifications to their order. They can also remove their entries.

If the database for the restaurant is incomplete and a user wants to add a menu item to the restaurants menu, he can do this with a button on the menu.

## Restaurant overview page

This page shows tiles for every restaurant known to the application. Similar to the orders page, restaurants are shown as tiles and the first tile, containing a plus sign can be used to add a restaurant.

## Restaurant page

Every restaurant page shows a form for the restaurant data, it's opwening hours and its menu. Users can add and remove categories to a menu and menu items. If no categories are defined, the items are ordered by restaurant specific ID and name.

## User page

This page is used to register new users and existing logged in users can use it to change their name, password and email address.
