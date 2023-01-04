# Intermediate docker image to build the bundle in and install dependencies
FROM balenalib/raspberrypi3-debian-node:19.0.1-buster-build as build

# Set the working directory to /usr/src/app
WORKDIR /usr/src/app

RUN git clone https://github.com/octera/powerflow-display.git .

# Install the dependencies
# Clean install because we want to install the exact versions
RUN npm ci

# Build the project
RUN npm run build

# Pull the same Node image and use it as the final (production image)
FROM balenalib/raspberrypi3-debian-node:19.0.1-buster as production

# Set the working directory to /usr/src/app
WORKDIR /usr/src/app

# Only copy the results from the build over to the final image
# We do this to keep the final image as small as possible
COPY --from=build /usr/src/app/node_modules ./node_modules
COPY --from=build /usr/src/app/server ./server
COPY --from=build /usr/src/app/dist ./dist

# Expose port 3000 (default port)
EXPOSE 3000

# Start the application
CMD [ "node", "server/entry.express"]




