# Technologies and frameworks

doenerstag uses go for the Backend and HTML, CSS and Typescript for the Web UI. Data is stored in a postgresql database. The software must run locally without requiring internet access to download additional data.

## Frontend

The primary user interface is a  web frontend using HTML, CSS and Typescript. It uses Tailwind CSS as CSS framework. The code resides in the folder "static" in the repository and contains subfolders for images, CSS and other resources like webfonts. Swagger is optionally available under the /tools/swagger URL.

The frontend is prepared for internationalization and switching languages. By default, English defaults and German translations are provided, The frontend uses the language preferred by the browser and English as fallback.

The application offers a dark and a bright mode. Dark mode is the default and can be switched in the UI.

The frontend uses cookies to store a users name, preferred language and their chouice of dark or bright mode.

## Backend

The backend is written in golang. It serves the frontend from the static folder as it's web root and provides a versioned REST API for the communication between the frontend and backend as well as third party applications. The REST API is documented as OpenAPI document.

There are tests using the golang test framework that covers all API endpoints.

The backend uses zerolog for structured logging. The log format uses json for every log message. The application uses the five log levels FATAL (1), ERROR (2), WARN (3), INFO (4), DEBUG (5). Every log level also loggs any event of the levels with a lower number. The log level FATAL logs errors that require terminating the application, ERROR logs errors that can't be compensated for. WARN is used for situations the application can heal and proceed with processing a request. The log level INFO logs all API requests and events. In log level DEBUG, on top of logging all the information of lower log levels, each function logs the function call with the provided parameters except for sensitive information like passwords. It also logs database queries.

The backend uses cobra and viper for handling parameters from a config file, environment variables prefixed with DOENER_ and commandline parameters. The application n be started with doenerstag, where the verb defines various modes as outlined below:

### Mode server

The verb "server" runs the https web server. It can optionally run without encr5yption as http server and serves the frontend and API. In this mode, the application needs the following parameters:

- --config or -c for the path to the configuration file. If none is provided, the default is doenerstag.yaml in the working directory
- --port or -p for the port to run the web server on, defaults to 8443
- --no-https to turn http mode on, defasults to false
- --no-swagger turns off the path to server the swagger UI, defaults to false

### Mode install

The verb "install" sets up the database and creates or updates a configuration file doenerstag.yaml containing all possible parameters and comments interactively.

When started with the verb "install", doenerstag reads an existing configuration file and suggests the values of the parameters therein as defaults when asking the user for the new values. If no configuration file exists or a parameter is not defined there, it assumes the default config file doenerstag.yaml in the working directory. Before asking questions, the application tests if it can write to the output file. If the output file is the same as the file specified by --config, the application ensures, that the file content is not erased during this test.

The database installation has the following parameters:

- --config or -c pointing to the configuration file.
- --output or -o specifiying the new configuration file. If not provided, it uses the value of --config or the default value doenerstag.yaml in the working directory.
- --database-root-user or -R provides a different database user with the rights to create the database and create or modify database-users. This parameter is never stored in the generated configuration file.
- Using --database-root-password on the commandline must trigger a FATAL security error. It can be provided via environment variable or during the interactive dialog with the user. This parameter is never stored in the generated configuration file.
- --database-admin-user or -A provides a user that owns the database and has full rights to create and modify the various objects within. This parameter is never stored in the generated configuration file.
- Using --database-admin-password on the commandline must trigger a FATAL security error. It can be provided via environment variable or during the interactive dialog with the user. This parameter is never stored in the generated configuration file.
- to connect to the database, the same parameters as above (--database-server, --database-port, --database-name and --database-user) are used. If no --database-password is provided via environment variable or the interactive dialog

### Update mode

Using the verb "update" will currently just output the informaton, that an update is currently not possible. After releasing the initial version, it will be used to update an existing configuration file and database.

It accepts the same parameters as the verb "install".

### Global parameters

All verbs also understand the following global parameters.

- --database-server or -S for the PostgreSQL database server name, defaults to localhost
- --database-port or -P for the port, the database is running on, defaults to 5432
- --database-name or -N for the database
- --database-user or -U for the username used for connecting to the database, defaults to doener
- Using --database-password on the commandline must trigger a FATAL security error, it can be provided in the configurtation file or as environment variable only
- --log-level or -l is one of FATAL, ERROR, WARN, INFO, DEBUG. It defaults to INFO, the application also handles converting lower to upper case
- --log-file or -f specifies the file, the log messages are going to. Default is stdout

## Database integration

The go backend stores its data in the postgresql 18 database. The database host, database name and database user/pqasswords must be configurable.
