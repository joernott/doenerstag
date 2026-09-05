# Prompt log

This file tracks the prompts given to the AI assistant during development of
doenerstag, in chronological order. It is a development artifact, not part of
the specification.

## 2026-09-05

### Prompt 1

> I want to create an application to coordinate food orders called doenerstag.
> The doenerstag repository will be used to develop the application and the docs
> folder contains my initial specification for it. Please review the current
> specification and suggest missing parts and improvements. Add all my prompts to
> a file zz_prompts.md in that folder to track them.

### Prompt 2

> Please modify the documentation based on the following answers:
>
> Answers to part A:
>
> 1. The application is run in a private network, where every uswer with access to
> the network should also have access to the application.
> Everyone can see the orders without logging in and can self-register.
> We should still implement session handling and CSRF
>
> 2. When building the application, we have internet access, however, having the
> build toolchain running locally is preferred over online services.
> The static folder will be build output. Embedding it into the application should
> be done before releasing the application. Keeping the folder outside during
> development allows me to fiddle with its content without having to rebuild and
> restart the backend at every step.
> Having a switch in the code to activate the use of go:embed instead of the
> external folder would be useful for that.
>
> 3. SSE per order sounds fine
>
> 4. Every user in the local network is trusted enough to add to the menu. This
> allows for "crowdsourcing" the application data rather than forcing an
> administrator to provide all the menu data. Having an admin user "root" for
> special operations like deleting restaurants, menu items, old orders etc makes
> sense.
> An audit trail should be provided by log messages when running on the INFO log
> level
>
> Answers to part B:
>
> Modifications should be mostly freetext, but having a a table with predefined
> selectable offers as you suggested is a good idea. Please add the definition for
> them.
>
> Please also add the snapshotted price to the order items.
>
> Also add a soft_delete field to menu items to preserve referential integrity. I
> do not intend to keep completed orders for more than a few days, so, when
> deleting old orders, we should also clean up those menu items that are marked
> for deletion and no longer referenced anywhere.
>
> Please add a definition for seeded table for the Allergens and additives and
> document a publicly available source from where we can seed the table.
>
> The free tags like "vegan" and "spicy" should be kept separately, filtering
> should be possible by both types of tags.
>
> We should store images in the database, including their metadata. The image
> upload size should be configurable in the configuration. The application should
> resize images to fit the layout. I would like to restrict the media types to
> JPEG, PNG and GIF files.
>
> Adding audit fields to every table is a good idea, please add them to the
> specification.
>
> Answers to part C:
>
> Title/Status: Add a date/time field for the order deadline. That time must be
> before the pickup/delivery time. Orders are active if the current time is before
> the time specified in that field.
> The title for the order as well as the text on the tile should be computed from
> the restaurant name, the date and time for pickup/delivery. The deadline should
> also be visible on the overview.
>
> Empty item list: Creating an order with an empty list should be possible.
>
> Missing constraints: Those constraints should be validated by the frontend
>
> Please assume full timestamps in UTC for every date/time field. These should be
> converted to local time when displaying them. As the application is aimed at
> small groups organizing local delivery or pickup, it is safe to assume that
> restaurant and the people placing orders are always in the same timezone.
>
> Delivery mode has no delivery address: As the order is placed manually based on
> the data gathered, there is no need to specify the delivery address in the
> application. It can be safely assumed that the person placing the order is well
> aw3are of their current location. Optional freetext fields to specify who will
> collect the money and who will do the pickup make sense.
> A minimum order value for delivery should be added as an optional field to the
> restaurant and copied to the order upon restaurant selection. The same goes for
> delivery fees.
>
> Money type: Always assume integers based on the smallest coins (cents) to be
> stored in the database. The user interface should display proper values.
> Specifying the currency per restaurant makes sense, The UI should take care to
> display the currency next to every price. Storing the currency in ISO format
> makes sense. There should be a table matching the 3 letter ID to a currency
> symbol to display.
>
> Category's Order field: Yes this should be renamed to sort_order and categories
> are unique to every restaurant.
>
> User: Users will be deleted immediately when the user deletes their account. On
> inactive orders, the items should be remapped to a "special" UUID
> 00000000-0000-7000-8000-000000000000 displayed at the "deleted user". When there
> are active orders where the user has added an item, he will be informed that
> this will also delete his entry from active orders and he will be given the
> chance to cancel at that point. Having an optional field for display name name
> is OK. If none is set, the chosen user name is used.
>
> Restaurant contacts: Having a child table fur one or more items was implied. A
> helper table for contact types, where something like "email", phone number",
> "address", "website" should help the frontend render the data correctly.
>
> Version table: Having an additional field for schema_version make sense. Adding
> an "applied_at" timestamp field makes sense, please add them.
>
> Answers to part D:
>
> TLS certificate and key parameters do not exist: Please add the parameters
> --tls-cert (-t) and --tls-key (-T) with the defaults pointing to the files
> server.crt and server.key in the current working directory.
> Also add --bind-address (-b) with a default to bind to all addresses.
>
> Also add --database-sslmode and a --max-connection-pool setting for limiting the
> maximum open/idle database connections as suggested.
>
> No password hashing algorithm: If Argon2id can be hashed inside the browser and
> checked inside the backend, use that for hashing the password. Otherwise use
> bcrypt and recommend the cost floor.
>
> No session mechanism: use JWT. Idle timeouts should be configurable with
> --idle-timeout (default 6h) and absolute timeouts with --absolute-timeout (7d)
>
> Specify CSRF protection, a reasonable CORS policy, security headers/CSP, and
> rate limiting on login. The application is run on internal networks with a low
> number of non-malicious users having access to it.
>
> API paths are prefixed with /api/v1), error responses are returned using the
> standard HTTP error codes containing a JSON object with an internal error number
> and a short human readable error message.
> As the number of items is usually low enough and the bandwidth in the local
> network is high, there is no need for pagination. If a menu is large, items will
> be grouped in categories.
> Filtering restaurants is not needed, filtering menu items can be done by
> selecting tags and Allergens.
> As every oser can only edit their own order item and only the creator of an
> order can make changes to the order itself, concurrent order edits should be
> rare. and restricting a user to one login at a time should solve the issue of
> one uswer accessing the same order from multiple browsers at the same time.
>
> Third-party API auth: Third party access is not the main focus for the first
> iteration of the application. Please specify a token authentication mechanism,
> where users can create and manage tokens used by that.
>
> No migration tooling named: Use golang-migrate for migration. and the "update"
> statement will be used for managing database schema updates after the initial
> release of the application. It will also be used to update the configuration
> file.
>
> No version verb or --version flag, despite a version page in the UI: Add a verb
> "version" that shows the last entry of the version table to the specification.
>
> Operational endpoints missing: Add a /api/v1/health endpoint detailing that
> returns a simple ok status, if the database connection is established.
> Add a /api/v1/metrics endpoint that provides the number of restaurants, menu
> items, open and expired orders, users and the number of database connections.
>
> Add an /api/v1/shutdown endpoint that shuts down the application. This endpoint
> can only be used by the admin user.
>
> HTTP read/write/idle timeouts, should be configurable, please add configuration
> parameters --http-read-timeout, --http-write-timeout, -http-idle-timeout with a
> default value of 300 seconds.
>
> Log rotation will be handled by logrotate, when using --log-file,
>
> Please specify adding request/correlation IDs in log lines.
>
> Retention for expired orders can be specified, using the --retention (-r)
> parameter. Add a verb "cleanup" that can be used by a cron job to periodically
> clean expired orders. In addition, the user who created an order or the
> administrator can remove it manually after it expired.
>
> PostgreSQL 18 will be the target version for the application. There is no need
> to support 16/17 as 18 has been released for almost a year now.
>
> Even though DEBUG "each function logs the function call with parameters" is
> extremely heavy, this never posed a problem in the past when using zerolog. It
> also helped pinpointing problems during development of other applications and
> should only be used under extreme circumstances.
>
> Answers to part E:
>
> Add a specification for the summary page. This page will be displayed by
> clicking on the summary button on the order page or a summary button integrated
> in the order tile on the order overview page.
>
> The application will mostly be used in web browsers on PCs running Windows /
> MAC. It should be responsive and useable on tablets and phones equally well but
> the strategy is not "mobile first" but "Firefox and Chrome first"
>
> There should be a Login/Register button in the title, when no user is logged in.
> When a user is logged in, it is replaced by the user name/handle and a logout
> symbol.
>
> There is no need for a "sharing mechanism", the app will be available in a
> company/club intranet and promoted through their channels.
>
> Having an "empty" state should always show only the tile for adding entries to
> the respective list. The application runs on a fast intranet/LAN environment,
> lazy loading, combined with a fast network should minimize the need for a
> "loading" state to the mechanism already provided by the browser and having a
> confirmation record for every destructive action together with restricting
> deletion of restaurants and menus to the administrator user and only allow the
> user who created an order and the admin to delete the order limits the impact
> there, too.
>
> No accessibility target: Please specify WCAG 2.1 AA and require keyboard
> navigation
>
> Language and locale precedence should be explicit: cookie -> Accept-Language ->
> English. Dates and times and numbers should be formatted per locale, too.
> However, the currency is tied to the restaurant and does not change based on
> nthe users language.
>
> No search or sort on restaurant/order overviews: As the number of restaurants
> usually used by the targeted audiences as well as the number of orders will be
> rather small (Expect between 3 and 10 restaurants and maybe 3 concurrent active
> orders per day), a search or filter functionality is currently not the focus for
> the application.
>
> Deadline notifications: That is currently not planned.
>
> The imprint page should be stored in a file as a HTML snippet. As the
> application is intended as an intranet tool and not publicly available, there is
> no legal requirement for it in that context. When logged in as an administrator,
> there should be a button visible to replace this content in the database. During
> the initial installation, the user should be asked for a file location for it.
>
> part F:
>
> A "legal notes" page should be created and handled in a similar matter to the
> imprint page, where the operator of the application can put the required
> information.
> Please add a document to the project where we specify what GDPR related and
> legal information
>
> part G:
>
> Please go ahead with your suggestion and restructure the documentation.
>
> part H:
>
> please act on all the typos and the broken sentence.
